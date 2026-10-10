// Package templates embeds the agency's PHP, MySQL and Nginx base configuration.
package templates

import (
	"bytes"
	"embed"
	"text/template"
)

//go:embed php.ini.tmpl php-fpm.conf.tmpl my.cnf.tmpl nginx.conf.tmpl pma.config.inc.php
var fs embed.FS

// Render executes the named template with data.
func Render(name string, data any) (string, error) {
	raw, err := fs.ReadFile(name)
	if err != nil {
		return "", err
	}
	t, err := template.New(name).Parse(string(raw))
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Raw returns an embedded file unchanged.
func Raw(name string) (string, error) {
	b, err := fs.ReadFile(name)
	return string(b), err
}
