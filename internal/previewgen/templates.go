// Package previewgen limits the initial-state adapter to private preview resource controllers.
package previewgen

import (
	"strings"

	"github.com/crossplane/upjet/v2/pkg/pipeline/templates"
)

const selected = `or (eq .ResourceType "cloudflare_zero_trust_access_service_token") (eq .ResourceType "cloudflare_zero_trust_tunnel_cloudflared") (eq .ResourceType "cloudflare_zero_trust_access_application") (eq .ResourceType "cloudflare_connectivity_directory_service")`

const accessGate = `or (eq .ResourceType "cloudflare_workers_script") (eq .ResourceType "cloudflare_workers_script_subdomain")`

func Configure() {
	template := templates.ControllerTemplate
	importAnchor := "\t{{ .Imports }}"
	initializerAnchor := "\tvar initializers managed.InitializerChain"
	connectorAnchor := "tjcontroller.NewConnector(mgr.GetClient(), o.WorkspaceStore,"
	if strings.Count(template, importAnchor) != 1 || strings.Count(template, connectorAnchor) != 1 || strings.Count(template, initializerAnchor) != 1 {
		panic("unexpected Upjet controller template")
	}
	template = strings.Replace(template, importAnchor, importAnchor+`{{ if `+selected+` }}
 statebootstrap "github.com/wildbitca/provider-upjet-cloudflare/internal/statebootstrap"{{ end }}{{ if `+accessGate+` }}
 workeraccess "github.com/wildbitca/provider-upjet-cloudflare/internal/workeraccess"{{ end }}`, 1)
	template = strings.Replace(template, initializerAnchor, initializerAnchor+`{{ if `+accessGate+` }}
	initializers = append(initializers, workeraccess.NewInitializer(mgr.GetAPIReader())){{ end }}`, 1)
	templates.ControllerTemplate = strings.Replace(template, connectorAnchor, `tjcontroller.NewConnector(mgr.GetClient(), {{ if `+selected+` }}statebootstrap.NewStore(o.WorkspaceStore){{ else }}o.WorkspaceStore{{ end }},`, 1)
}
