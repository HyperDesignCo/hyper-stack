package nginx

import (
	"strings"
	"testing"
)

func site() Site {
	return Site{Domain: "bazooka.test", Root: "C:/work/bazooka", FPMAddr: "127.0.0.1:9083",
		CertFile: "C:/hs/certs/bazooka.test.pem", KeyFile: "C:/hs/certs/bazooka.test-key.pem", LogDir: "C:/hs/logs"}
}

func TestRender(t *testing.T) {
	out, err := Render(site())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"server_name bazooka.test;",
		`root "C:/work/bazooka/public";`,
		"fastcgi_pass 127.0.0.1:9083;",
		`ssl_certificate "C:/hs/certs/bazooka.test.pem";`,
		"try_files $uri $uri/ /index.php?$query_string;",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestRenderRejectsEmptyField(t *testing.T) {
	s := site()
	s.FPMAddr = ""
	if _, err := Render(s); err == nil {
		t.Fatal("expected error")
	}
}
