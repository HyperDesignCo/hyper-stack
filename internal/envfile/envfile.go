// Package envfile edits managed keys in a .env file without touching anything else.
package envfile

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ManagedKeys are the only keys Hyper Stack may change.
var ManagedKeys = []string{
	"APP_URL", "DB_CONNECTION", "DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USERNAME", "DB_PASSWORD",
	"REDIS_HOST", "REDIS_PORT", "MAIL_MAILER", "MAIL_HOST", "MAIL_PORT",
}

var lineRe = regexp.MustCompile(`^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=`)

// Apply updates or appends the given values in content. Keys not in ManagedKeys
// are rejected. Order, comments and unmanaged lines are preserved byte for byte.
func Apply(content string, values map[string]string) (string, error) {
	for k := range values {
		if !isManaged(k) {
			return "", fmt.Errorf("refusing to edit unmanaged key %s", k)
		}
	}
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.Split(content, nl)
	done := map[string]bool{}
	for i, l := range lines {
		m := lineRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if v, ok := values[m[1]]; ok && !done[m[1]] {
			lines[i] = m[1] + "=" + quote(v)
			done[m[1]] = true
		}
	}
	var add []string
	for _, k := range ManagedKeys {
		if v, ok := values[k]; ok && !done[k] {
			add = append(add, k+"="+quote(v))
		}
	}
	if len(add) > 0 {
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = append(lines[:n-1], append(add, "")...)
		} else {
			lines = append(lines, add...)
		}
	}
	return strings.Join(lines, nl), nil
}

// UpdateFile prepares dir/.env (copying .env.example if missing), backs the
// original up to .env.hyper-backup (first time only) and applies values.
func UpdateFile(dir string, values map[string]string) error {
	envPath := filepath.Join(dir, ".env")
	data, err := os.ReadFile(envPath)
	if os.IsNotExist(err) {
		data, err = os.ReadFile(filepath.Join(dir, ".env.example"))
		if err != nil {
			return fmt.Errorf("no .env or .env.example in %s: create one, then run again", dir)
		}
	} else if err != nil {
		return err
	} else {
		backup := filepath.Join(dir, ".env.hyper-backup")
		if _, e := os.Stat(backup); os.IsNotExist(e) {
			if err := os.WriteFile(backup, data, 0o600); err != nil {
				return err
			}
		}
	}
	out, err := Apply(string(data), values)
	if err != nil {
		return err
	}
	if out == string(data) {
		if _, e := os.Stat(envPath); e == nil {
			return nil
		}
	}
	return os.WriteFile(envPath, []byte(out), 0o600)
}

func isManaged(k string) bool {
	for _, m := range ManagedKeys {
		if m == k {
			return true
		}
	}
	return false
}

func quote(v string) string {
	if v == "" || strings.ContainsAny(v, " #\"'$") {
		return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(v) + `"`
	}
	return v
}
