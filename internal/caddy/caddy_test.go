package caddy

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	out, err := Render(Site{Domain: "a.test", Root: "/w/a", FPMAddr: "127.0.0.1:9083", CertFile: "/c/a.pem", KeyFile: "/c/a-key.pem", LogDir: "/l"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"a.test {", "tls /c/a.pem /c/a-key.pem", "root * /w/a/public", "php_fastcgi 127.0.0.1:9083", "redir https://a.test{uri}"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestRenderRejectsEmpty(t *testing.T) {
	if _, err := Render(Site{Domain: "a.test"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestMainImportsSites(t *testing.T) {
	if !strings.Contains(Main("/s"), "import /s/*.caddy") {
		t.Fatal("main caddyfile must import site files")
	}
}
