package previewgen

import (
	"bytes"
	"go/format"
	"strings"
	"testing"
	"text/template"

	"github.com/crossplane/upjet/v2/pkg/pipeline/templates"
)

func render(t *testing.T, source, resource string) []byte {
	t.Helper()
	values := map[string]any{"Header": "// fixture", "GenStatement": "// generated", "Package": "test", "Imports": "", "CRD": map[string]string{"Kind": "Example"}, "ResourceType": resource, "TypePackageAlias": "v1alpha1.", "FeaturesPackageAlias": "features."}
	tmpl, err := template.New("controller").Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, values); err != nil {
		t.Fatal(err)
	}
	formatted, err := format.Source(out.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	return formatted
}
func TestOnlyIDBasedPreviewControllersUseAdapter(t *testing.T) {
	original := templates.ControllerTemplate
	t.Cleanup(func() { templates.ControllerTemplate = original })
	Configure()
	for _, resource := range []string{"cloudflare_dns_record", "cloudflare_workers_script", "cloudflare_worker_version", "cloudflare_r2_bucket"} {
		if !bytes.Equal(render(t, original, resource), render(t, templates.ControllerTemplate, resource)) {
			t.Fatalf("changed unrelated controller %s", resource)
		}
	}
	for _, resource := range []string{"cloudflare_zero_trust_access_service_token", "cloudflare_zero_trust_tunnel_cloudflared", "cloudflare_zero_trust_access_application", "cloudflare_connectivity_directory_service"} {
		if !strings.Contains(string(render(t, templates.ControllerTemplate, resource)), "statebootstrap.NewStore(o.WorkspaceStore)") {
			t.Fatalf("missing adapter for %s", resource)
		}
	}
}
