package statebootstrap

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/spf13/afero"
	zero "github.com/wildbitca/provider-upjet-cloudflare/apis/namespaced/zero/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func token() *zero.TrustAccessServiceToken {
	tr := &zero.TrustAccessServiceToken{}
	tr.SetName("gateway-relay")
	tr.SetNamespace("preview")
	tr.SetUID(types.UID("bootstrap-test-uid"))
	return tr
}
func state(id any) map[string]any {
	return map[string]any{"version": 4, "lineage": "bootstrap-test-uid", "serial": 1, "resources": []any{map[string]any{
		"mode": "managed", "type": "cloudflare_zero_trust_access_service_token", "name": "gateway-relay",
		"instances": []any{map[string]any{"schema_version": 0, "attributes": map[string]any{"id": id, "name": "relay"}}},
	}}}
}
func TestOnlySyntheticEmptyIDStateIsRemoved(t *testing.T) {
	for _, tc := range []struct {
		name          string
		modify        func(map[string]any)
		clears, fails bool
	}{
		{"new", func(map[string]any) {}, true, false},
		{"existing-id", func(s map[string]any) { s["resources"] = state("real-id")["resources"] }, false, false},
		{"unknown-id", func(s map[string]any) { s["resources"] = state(nil)["resources"] }, false, false},
		{"other-lineage", func(s map[string]any) { s["lineage"] = "different-uid" }, false, true},
		{"multiple-resources", func(s map[string]any) {
			s["resources"] = append(s["resources"].([]any), state("")["resources"].([]any)[0])
		}, false, true},
		{"other-resource", func(s map[string]any) { s["resources"].([]any)[0].(map[string]any)["name"] = "other" }, false, true},
		{"already-empty", func(s map[string]any) { s["resources"] = []any{} }, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.Afero{Fs: afero.NewMemMapFs()}
			dir := "/workspace"
			if err := fs.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			s := state("")
			tc.modify(s)
			before, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "terraform.tfstate")
			if err := fs.WriteFile(path, before, 0600); err != nil {
				t.Fatal(err)
			}
			err = clearSyntheticState(fs, dir, token())
			if (err != nil) != tc.fails {
				t.Fatalf("unexpected adapter error: %v", err)
			}
			after, err := fs.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.clears {
				if string(before) != string(after) {
					t.Fatal("changed established or ambiguous state")
				}
				return
			}
			var got map[string]any
			if err := json.Unmarshal(after, &got); err != nil {
				t.Fatal(err)
			}
			if len(got["resources"].([]any)) != 0 || got["lineage"] != s["lineage"] || got["serial"] != float64(1) {
				t.Fatal("did not preserve state metadata while removing synthetic instance")
			}
		})
	}
}

func TestRealUpjetStoreStartsNewResourceWithoutSyntheticInstance(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	tr := token()
	name, account := "relay", "account"
	tr.Spec.ForProvider.Name = &name
	tr.Spec.ForProvider.AccountID = &account
	cfg := &config.Resource{TerraformResource: &schema.Resource{Schema: map[string]*schema.Schema{
		"id": {Type: schema.TypeString, Computed: true}, "name": {Type: schema.TypeString, Required: true}, "account_id": {Type: schema.TypeString, Required: true},
	}}, ExternalName: config.IdentifierFromProvider}
	setup := terraform.Setup{Version: "1.11.0", Requirement: terraform.ProviderRequirement{Source: "cloudflare/cloudflare", Version: "5.28.1"}}
	base := terraform.NewWorkspaceStore(logging.NewNopLogger(), terraform.WithDisableInit(true))
	if _, err := NewStore(base).Workspace(t.Context(), nil, tr, setup, cfg); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(os.TempDir(), string(tr.GetUID()), "terraform.tfstate")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got["resources"].([]any)) != 0 {
		t.Fatal("native refresh would see the uncreated resource")
	}
	// An explicit external identifier still follows the native adoption path.
	meta.SetExternalName(tr, "existing-token")
	if _, err := NewStore(base).Workspace(t.Context(), nil, tr, setup, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	attrs := got["resources"].([]any)[0].(map[string]any)["instances"].([]any)[0].(map[string]any)["attributes"].(map[string]any)
	if attrs["id"] != "existing-token" {
		t.Fatal("adapter changed the native adoption identifier")
	}
}

type stubStore struct{ workspace *terraform.Workspace }

func (s stubStore) Workspace(context.Context, resource.SecretClient, resource.Terraformed, terraform.Setup, *config.Resource) (*terraform.Workspace, error) {
	return s.workspace, nil
}

func TestEstablishedDeletingAndRunningWorkspacesAreUntouched(t *testing.T) {
	for _, kind := range []string{"external-name", "deleting", "observed", "running"} {
		t.Run(kind, func(t *testing.T) {
			tr := token()
			fs := afero.Afero{Fs: afero.NewMemMapFs()}
			dir := filepath.Join(fs.GetTempDir(""), string(tr.GetUID()))
			if err := fs.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(state(""))
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "terraform.tfstate")
			if err := fs.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			workspace := terraform.NewWorkspace(dir)
			switch kind {
			case "external-name":
				meta.SetExternalName(tr, "existing")
			case "deleting":
				now := metav1.Now()
				tr.SetDeletionTimestamp(&now)
			case "observed":
				if err := tr.SetObservation(map[string]any{"client_id": "known-client"}); err != nil {
					t.Fatal(err)
				}
			case "running":
				workspace.LastOperation.MarkStart("apply")
			}
			store := NewStore(stubStore{workspace: workspace})
			store.fs = fs
			if _, err := store.Workspace(t.Context(), nil, tr, terraform.Setup{}, nil); err != nil {
				t.Fatal(err)
			}
			after, err := fs.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(raw) {
				t.Fatal("adapter edited an established, deleting or running workspace")
			}
		})
	}
}
