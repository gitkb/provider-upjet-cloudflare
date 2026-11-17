// Package preview supplies references and connection outputs for private Worker previews.
package preview

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/crossplane/crossplane-runtime/v2/pkg/reference"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	"github.com/crossplane/upjet/v2/pkg/config"
	"github.com/crossplane/upjet/v2/pkg/resource"
	corev1 "k8s.io/api/core/v1"
)

func Configure(p *config.Provider) {
	p.AddResourceConfigurator("cloudflare_connectivity_directory_service", func(r *config.Resource) {
		r.References["host.network.tunnel_id"] = config.Reference{TerraformName: "cloudflare_zero_trust_tunnel_cloudflared"}
		r.References["host.resolver_network.tunnel_id"] = config.Reference{TerraformName: "cloudflare_zero_trust_tunnel_cloudflared"}
	})
	p.AddResourceConfigurator("cloudflare_workers_script", func(r *config.Resource) {
		r.References["bindings.service_id"] = config.Reference{TerraformName: "cloudflare_connectivity_directory_service"}
		r.References["script_name"] = config.Reference{
			TerraformName: "cloudflare_zero_trust_access_application",
			RefFieldName:  "AccessApplicationRef", SelectorFieldName: "AccessApplicationSelector",
			Extractor: "github.com/wildbitca/provider-upjet-cloudflare/config/namespaced/preview.AccessWorkerName()",
		}
	})
	p.AddResourceConfigurator("cloudflare_zero_trust_access_application", func(r *config.Resource) {
		r.References["policies.include.service_token.token_id"] = config.Reference{TerraformName: "cloudflare_zero_trust_access_service_token"}
	})
	p.AddResourceConfigurator("cloudflare_workers_script_subdomain", func(r *config.Resource) {
		// Do not enable the edge endpoint until its service-only Access policy is ready.
		r.References["script_name"] = config.Reference{
			TerraformName: "cloudflare_zero_trust_access_application",
			RefFieldName:  "AccessApplicationRef", SelectorFieldName: "AccessApplicationSelector",
			Extractor: "github.com/wildbitca/provider-upjet-cloudflare/config/namespaced/preview.AccessWorkerName()",
		}
	})
	p.AddResourceConfigurator("cloudflare_zero_trust_tunnel_cloudflared", func(r *config.Resource) {
		r.Sensitive.AdditionalConnectionDetailsFn = TunnelCredentials
	})
	p.AddResourceConfigurator("cloudflare_zero_trust_access_service_token", func(r *config.Resource) {
		r.Sensitive.AdditionalConnectionDetailsFn = func(attrs map[string]any) (map[string][]byte, error) {
			out := map[string][]byte{}
			for _, key := range []string{"client_id", "client_secret"} {
				if value, ok := attrs[key].(string); ok && value != "" {
					out[key] = []byte(value)
				}
			}
			return out, nil
		}
	})
}

// TunnelCredentials exports the native cloudflared credential file through its connection Secret.
func TunnelCredentials(attrs map[string]any) (map[string][]byte, error) {
	values := map[string]string{}
	for field, key := range map[string]string{"account_id": "AccountTag", "id": "TunnelID", "tunnel_secret": "TunnelSecret"} {
		value, _ := attrs[field].(string)
		if value == "" {
			return nil, nil
		}
		values[key] = value
	}
	data, err := json.Marshal(values)
	return map[string][]byte{"credentials.json": data, "tunnel_id": []byte(values["TunnelID"])}, err
}

// AccessWorkerName resolves only a ready policy protecting exactly one workers.dev hostname.
var workerName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func AccessWorkerName() reference.ExtractValueFn {
	return func(mg xpresource.Managed) string {
		if mg.GetCondition(xpv2.TypeReady).Status != corev1.ConditionTrue || mg.GetCondition(xpv2.TypeSynced).Status != corev1.ConditionTrue {
			return ""
		}
		tr, ok := mg.(resource.Terraformed)
		if !ok {
			return ""
		}
		attrs, err := tr.GetObservation()
		if err != nil {
			return ""
		}
		domain, _ := attrs["domain"].(string)
		parts := strings.Split(domain, ".")
		if len(parts) != 4 || parts[2] != "workers" || parts[3] != "dev" || !workerName.MatchString(parts[0]) {
			return ""
		}
		return parts[0]
	}
}
