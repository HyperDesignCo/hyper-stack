// Package nginx renders per-site vhost files from a template.
package nginx

import (
	"bytes"
	"fmt"
	"text/template"
)

// Site holds everything a vhost needs. Paths must already use forward slashes
// (nginx accepts them on Windows too).
type Site struct {
	Domain   string
	Root     string // project root; the web root is Root/public unless WebRoot is set
	WebRoot  string // optional override (e.g. phpMyAdmin has no public/ folder)
	FPMAddr  string // upstream name or host:port
	CertFile string
	KeyFile  string
	LogDir   string
	// Ports; zero means 80 / 443.
	HTTPPort  int
	HTTPSPort int
}

const vhostTmpl = `# Managed by Hyper Stack. Do not edit.
server {
    listen {{.HTTPPort}};
    server_name {{.Domain}};
    return 301 https://$host:{{.HTTPSPort}}$request_uri;
}

server {
    listen {{.HTTPSPort}} ssl;
    http2 on;
    server_name {{.Domain}};
    root "{{.WebRoot}}";
    index index.php index.html;

    ssl_certificate "{{.CertFile}}";
    ssl_certificate_key "{{.KeyFile}}";

    access_log "{{.LogDir}}/{{.Domain}}.access.log";
    error_log "{{.LogDir}}/{{.Domain}}.error.log";

    client_max_body_size 64M;

    location / {
        try_files $uri $uri/ /index.php?$query_string;
    }

    location ~ \.php$ {
        fastcgi_pass {{.FPMAddr}};
        fastcgi_param SCRIPT_FILENAME $realpath_root$fastcgi_script_name;
        include fastcgi_params;
    }

    location ~ /\.(?!well-known).* {
        deny all;
    }
}
`

var tmpl = template.Must(template.New("vhost").Parse(vhostTmpl))

// Render returns the vhost configuration for s.
func Render(s Site) (string, error) {
	for name, v := range map[string]string{"Domain": s.Domain, "Root": s.Root, "FPMAddr": s.FPMAddr, "CertFile": s.CertFile, "KeyFile": s.KeyFile, "LogDir": s.LogDir} {
		if v == "" {
			return "", fmt.Errorf("nginx vhost: %s is empty", name)
		}
	}
	if s.WebRoot == "" {
		s.WebRoot = s.Root + "/public"
	}
	if s.HTTPPort == 0 {
		s.HTTPPort = 80
	}
	if s.HTTPSPort == 0 {
		s.HTTPSPort = 443
	}
	var b bytes.Buffer
	if err := tmpl.Execute(&b, s); err != nil {
		return "", err
	}
	return b.String(), nil
}
