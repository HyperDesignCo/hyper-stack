package caddy

import (
	"strings"
	"testing"
)

func site() Site {
	return Site{Domain: "a.test", WebRoot: "/w/a/public", FPMAddr: "127.0.0.1:9083", CertFile: "/c/a.pem", KeyFile: "/c/a-key.pem", LogDir: "/l",
		HTTPPort: 8080, HTTPSPort: 8443}
}

func TestRender(t *testing.T) {
	out, err := Render(site())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"https://a.test:8443 {",
		`tls "/c/a.pem" "/c/a-key.pem"`,
		`root * "/w/a/public"`,
		"php_fastcgi 127.0.0.1:9083",
		"http://a.test:8080 {",
		"redir https://a.test:8443{uri} permanent",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// Caddy uses Go regular expressions, which have no look-ahead. A rule such as
// "^/\.(?!well-known)" is rejected by Caddy at startup.
func TestRenderUsesNoLookahead(t *testing.T) {
	out, _ := Render(site())
	if strings.Contains(out, "(?!") || strings.Contains(out, "(?=") {
		t.Fatalf("Go regular expressions have no look-ahead:\n%s", out)
	}
	if !strings.Contains(out, "not path /.well-known/*") {
		t.Error(".well-known must stay reachable")
	}
}

func TestStandardPortsAreOmittedFromTheRedirect(t *testing.T) {
	s := site()
	s.HTTPPort, s.HTTPSPort = 0, 0
	out, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "redir https://a.test{uri} permanent") || !strings.Contains(out, "http://a.test:80 {") {
		t.Errorf("default ports:\n%s", out)
	}
}

func TestRenderFallsBackToPublicUnderRoot(t *testing.T) {
	s := site()
	s.WebRoot, s.Root = "", "/w/a"
	out, err := Render(s)
	if err != nil || !strings.Contains(out, `root * "/w/a/public"`) {
		t.Fatalf("%v\n%s", err, out)
	}
}

func TestRenderRejectsEmpty(t *testing.T) {
	if _, err := Render(Site{Domain: "a.test"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestMainImportsSitesAndFixesPorts(t *testing.T) {
	m := Main("/s", 8080, 8443, "127.0.0.1:2619")
	for _, want := range []string{`import "/s/*.caddy"`, "auto_https off", "http_port 8080", "https_port 8443", "admin 127.0.0.1:2619"} {
		if !strings.Contains(m, want) {
			t.Errorf("missing %q in:\n%s", want, m)
		}
	}
}
