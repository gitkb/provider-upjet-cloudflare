package preview_test

import (
	"encoding/json"
	"testing"

	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	zero "github.com/wildbitca/provider-upjet-cloudflare/apis/namespaced/zero/v1alpha1"
	preview "github.com/wildbitca/provider-upjet-cloudflare/config/namespaced/preview"
)

func TestWorkerEndpointRequiresReadyAccessPolicy(t *testing.T) {
	app := &zero.TrustAccessApplication{}
	if err := app.SetObservation(map[string]any{"domain": "gateway-pr-201.hi-f3c.workers.dev", "policies": []any{map[string]any{"decision": "non_identity", "include": []any{map[string]any{"service_token": map[string]any{"token_id": "relay"}}}}}}); err != nil {
		t.Fatal(err)
	}
	extract := preview.AccessWorkerName()
	if got := extract(app); got != "" {
		t.Fatal("unready Access application enabled a Worker endpoint")
	}
	app.SetConditions(xpv2.Available(), xpv2.ReconcileSuccess())
	if got := extract(app); got != "gateway-pr-201" {
		t.Fatalf("wrong Worker name: %q", got)
	}
	for _, domain := range []string{"*.hi-f3c.workers.dev", "gateway.example.com", "gateway.hi-f3c.workers.dev/path"} {
		if err := app.SetObservation(map[string]any{"domain": domain, "policies": []any{map[string]any{"decision": "non_identity", "include": []any{map[string]any{"service_token": map[string]any{"token_id": "relay"}}}}}}); err != nil {
			t.Fatal(err)
		}
		if got := extract(app); got != "" {
			t.Fatalf("accepted an unprotected or ambiguous hostname %q", domain)
		}
	}
}

func TestTunnelConnectionSecret(t *testing.T) {
	attrs := map[string]any{"account_id": "account", "id": "tunnel", "tunnel_secret": "synthetic-secret"}
	out, err := preview.TunnelCredentials(attrs)
	if err != nil {
		t.Fatal(err)
	}
	var credentials map[string]string
	if err := json.Unmarshal(out["credentials.json"], &credentials); err != nil {
		t.Fatal(err)
	}
	if credentials["AccountTag"] != "account" || credentials["TunnelID"] != "tunnel" || credentials["TunnelSecret"] != "synthetic-secret" {
		t.Fatal("incomplete cloudflared credentials")
	}
	delete(attrs, "tunnel_secret")
	out, err = preview.TunnelCredentials(attrs)
	if err != nil || len(out) != 0 {
		t.Fatal("published partial tunnel credentials")
	}
}
