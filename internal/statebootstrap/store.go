// Package statebootstrap handles the native Cloudflare provider's initial ID requirement.
package statebootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/controller"
	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	"github.com/spf13/afero"
)

// Store preserves Upjet's workspace lifecycle but drops its synthetic empty-ID
// instance before the native provider tries to read a resource that is not created.
// Generated controllers select it only for the four preview resources needing IDs.
type Store struct {
	controller.Store
	fs afero.Afero
}

// NewStore adapts the default Upjet workspace store for an uncreated ID resource.
func NewStore(delegate controller.Store) *Store {
	return &Store{Store: delegate, fs: afero.Afero{Fs: afero.NewOsFs()}}
}

func (s *Store) Workspace(ctx context.Context, client resource.SecretClient, tr resource.Terraformed, setup terraform.Setup, cfg *config.Resource) (*terraform.Workspace, error) {
	w, err := s.Store.Workspace(ctx, client, tr, setup, cfg)
	if err != nil {
		return nil, err
	}
	if w.LastOperation.IsRunning() || tr.GetDeletionTimestamp() != nil || meta.GetExternalName(tr) != "" {
		return w, nil
	}
	observed, err := tr.GetObservation()
	if err != nil {
		return nil, err
	}
	if len(observed) != 0 {
		return w, nil
	}
	// This is Upjet v2.5.1's workspace location (tested using the actual store).
	// The immutable server-assigned UID identifies the directory and state lineage.
	dir, err := workspaceDirectory(s.fs, string(tr.GetUID()))
	if err != nil {
		return nil, err
	}
	if err := clearSyntheticState(s.fs, dir, tr); err != nil {
		return nil, err
	}
	return w, nil
}

func workspaceDirectory(fs afero.Afero, uid string) (string, error) {
	if uid == "" || strings.ContainsAny(uid, `/\`) || filepath.Base(uid) != uid || uid == "." || uid == ".." {
		return "", fmt.Errorf("invalid workspace UID")
	}
	return filepath.Join(fs.GetTempDir(""), uid), nil
}

type workspaceState struct {
	Version   int    `json:"version"`
	Lineage   string `json:"lineage"`
	Resources []struct {
		Mode      string `json:"mode"`
		Type      string `json:"type"`
		Name      string `json:"name"`
		Instances []struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"instances"`
	} `json:"resources"`
}

func (s workspaceState) validate(tr resource.Terraformed) error {
	if s.Version != 4 || s.Lineage != string(tr.GetUID()) || len(s.Resources) != 1 {
		return fmt.Errorf("unexpected workspace state identity")
	}
	r := s.Resources[0]
	if r.Mode != "managed" || r.Type != tr.GetTerraformResourceType() || r.Name != tr.GetName() || len(r.Instances) != 1 {
		return fmt.Errorf("unexpected workspace resource identity")
	}
	return nil
}

func clearSyntheticState(fs afero.Afero, dir string, tr resource.Terraformed) error {
	path := filepath.Join(dir, "terraform.tfstate")
	raw, err := fs.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	next, err := withoutSyntheticInstance(raw, tr)
	if err != nil || next == nil {
		return err
	}
	return atomicReplace(fs, dir, path, next)
}

func withoutSyntheticInstance(raw []byte, tr resource.Terraformed) ([]byte, error) {
	var state workspaceState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("invalid workspace state")
	}
	if len(state.Resources) == 0 {
		return nil, nil
	}
	if err := state.validate(tr); err != nil {
		return nil, err
	}
	// Never alter an instance with a real ID, including an apply whose result has
	// not yet been copied into Kubernetes annotations/status.
	id, exists := state.Resources[0].Instances[0].Attributes["id"]
	if !exists || id != "" {
		return nil, nil
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("invalid workspace document")
	}
	document["resources"] = json.RawMessage("[]")
	return json.Marshal(document)
}

func atomicReplace(fs afero.Afero, dir, path string, next []byte) error {
	file, err := afero.TempFile(fs.Fs, dir, ".state-bootstrap-")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer func() { _ = fs.Remove(temporary) }()
	if err := fs.Chmod(temporary, 0600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(next); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return fs.Rename(temporary, path)
}
