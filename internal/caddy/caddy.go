// Package caddy renders site blocks for the macOS web server.
package caddy

import (
	"bytes"
	"fmt"
	"text/template"
)

// Site mirrors nginx.Site so the runtime can switch web servers.
type Site struct {
	Domain   string
	Root     string // project root; the web root is Root/public
	FPMAddr  string
	CertFile string
	KeyFile  string
	LogDir   string
}

const siteTmpl = `# Managed by Hyper Stack. Do not edit.
{{.Domain}} {
    tls "{{.CertFile}}" "{{.KeyFile}}"
    root * "{{.Root}}/public"
    encode gzip
    request_body {
        max_size 64MB
    }
    @hidden path_regexp hidden ^/\.(?!well-known)
    respond @hidden 403
    php_fastcgi {{.FPMAddr}}
    file_server
    log {
        output file "{{.LogDir}}/{{.Domain}}.access.log"
    }
}

http://{{.Domain}} {
    redir https://{{.Domain}}{uri} permanent
}
`

var tmpl = template.Must(template.New("site").Parse(siteTmpl))

// Render returns the Caddyfile block for s.
func Render(s Site) (string, error) {
	for name, v := range map[string]string{"Domain": s.Domain, "Root": s.Root, "FPMAddr": s.FPMAddr, "CertFile": s.CertFile, "KeyFile": s.KeyFile, "LogDir": s.LogDir} {
		if v == "" {
			return "", fmt.Errorf("caddy site: %s is empty", name)
		}
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, s); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Main returns the top-level Caddyfile that imports every site file in dir.
// Automatic HTTPS is disabled because we supply our own certificates.
func Main(sitesDir string) string {
	return "{\n    auto_https disable_redirects\n    admin localhost:2019\n}\n\nimport " + sitesDir + "/*.caddy\n"
}
