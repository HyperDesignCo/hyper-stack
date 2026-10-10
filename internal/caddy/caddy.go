// Package caddy renders the web server configuration used on macOS.
package caddy

import (
	"bytes"
	"fmt"
	"text/template"
)

// Site is one project served by Caddy.
type Site struct {
	Domain    string
	Root      string // project root, used when WebRoot is empty
	WebRoot   string // the folder to serve (public/, or the project folder)
	FPMAddr   string // host:port of the PHP-FPM pool
	CertFile  string
	KeyFile   string
	LogDir    string
	HTTPPort  int // zero means 80
	HTTPSPort int // zero means 443
}

// The hidden-file rule uses two matchers because Go's regular expressions have
// no look-ahead (which is what "dotfiles except .well-known" would need).
const siteTmpl = `# Managed by Hyper Stack. Do not edit.
https://{{.Domain}}:{{.HTTPSPort}} {
    tls "{{.CertFile}}" "{{.KeyFile}}"
    root * "{{.WebRoot}}"
    encode gzip
    request_body {
        max_size 64MB
    }
    @hidden {
        path_regexp ^/\.
        not path /.well-known/*
    }
    respond @hidden 403
    php_fastcgi {{.FPMAddr}}
    file_server
    log {
        output file "{{.LogDir}}/{{.Domain}}.access.log"
    }
}

http://{{.Domain}}:{{.HTTPPort}} {
    redir https://{{.Domain}}{{if ne .HTTPSPort 443}}:{{.HTTPSPort}}{{end}}{uri} permanent
}
`

var tmpl = template.Must(template.New("site").Parse(siteTmpl))

// Render returns the Caddyfile block for s.
func Render(s Site) (string, error) {
	if s.WebRoot == "" && s.Root != "" {
		s.WebRoot = s.Root + "/public"
	}
	if s.HTTPPort == 0 {
		s.HTTPPort = 80
	}
	if s.HTTPSPort == 0 {
		s.HTTPSPort = 443
	}
	for name, v := range map[string]string{"Domain": s.Domain, "WebRoot": s.WebRoot, "FPMAddr": s.FPMAddr, "CertFile": s.CertFile, "KeyFile": s.KeyFile, "LogDir": s.LogDir} {
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

// Main returns the top-level Caddyfile that imports every site file in sitesDir.
// Certificates are supplied per site, so automatic HTTPS is off. admin is the
// address of the admin endpoint used to reload the configuration.
func Main(sitesDir string, httpPort, httpsPort int, admin string) string {
	return fmt.Sprintf("{\n    admin %s\n    auto_https off\n    http_port %d\n    https_port %d\n}\n\nimport \"%s/*.caddy\"\n",
		admin, httpPort, httpsPort, sitesDir)
}
