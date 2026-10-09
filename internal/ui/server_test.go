package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperdesign/hyper-stack/internal/app"
	"github.com/hyperdesign/hyper-stack/internal/config"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	p := config.PathsFor(filepath.Join(t.TempDir(), "hs"))
	s := New(func(out io.Writer) (*app.App, error) { return app.NewAt(p, out, false) })
	s.port = 7480
	return s
}

func do(h http.Handler, method, target, host string, hdr map[string]string, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = host
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func cookieFor(s *Server) map[string]string {
	return map[string]string{"Cookie": cookieName + "=" + s.token}
}

func TestRejectsWrongHost(t *testing.T) {
	s := testServer(t)
	h := s.Handler()
	for _, host := range []string{"evil.example:7480", "127.0.0.1:9999", "127.0.0.1"} {
		if w := do(h, "GET", "/api/state", host, cookieFor(s), ""); w.Code != http.StatusForbidden {
			t.Errorf("host %q: got %d, want 403 (DNS rebinding guard)", host, w.Code)
		}
	}
}

func TestAPIRequiresToken(t *testing.T) {
	s := testServer(t)
	h := s.Handler()
	if w := do(h, "GET", "/api/state", "127.0.0.1:7480", nil, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie: got %d", w.Code)
	}
	if w := do(h, "GET", "/api/state", "127.0.0.1:7480", map[string]string{"Cookie": cookieName + "=wrong"}, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong cookie: got %d", w.Code)
	}
	if w := do(h, "GET", "/api/state", "127.0.0.1:7480", cookieFor(s), ""); w.Code != http.StatusOK {
		t.Fatalf("valid cookie: got %d: %s", w.Code, w.Body.String())
	}
}

func TestTokenURLSetsCookieThenRedirects(t *testing.T) {
	s := testServer(t)
	h := s.Handler()
	w := do(h, "GET", "/?t="+s.token, "127.0.0.1:7480", nil, "")
	if w.Code != http.StatusFound {
		t.Fatalf("got %d", w.Code)
	}
	c := w.Result().Cookies()
	if len(c) != 1 || !c[0].HttpOnly || c[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie must be HttpOnly + SameSite=Strict: %+v", c)
	}
	if w := do(h, "GET", "/?t=nope", "127.0.0.1:7480", nil, ""); w.Code != http.StatusForbidden {
		t.Fatalf("bad token: got %d", w.Code)
	}
	if w := do(h, "GET", "/", "127.0.0.1:7480", nil, ""); w.Code != http.StatusForbidden {
		t.Fatalf("page without cookie: got %d", w.Code)
	}
}

func TestPostNeedsCustomHeaderAndSameOrigin(t *testing.T) {
	s := testServer(t)
	h := s.Handler()
	ok := cookieFor(s)

	if w := do(h, "POST", "/api/config", "127.0.0.1:7480", ok, `{"key":"http_port","value":"8080"}`); w.Code != http.StatusForbidden {
		t.Errorf("missing X-Hyper: got %d, want 403", w.Code)
	}
	cross := map[string]string{"Cookie": ok["Cookie"], "X-Hyper": "1", "Origin": "https://evil.example"}
	if w := do(h, "POST", "/api/config", "127.0.0.1:7480", cross, `{"key":"http_port","value":"8080"}`); w.Code != http.StatusForbidden {
		t.Errorf("cross-site Origin: got %d, want 403", w.Code)
	}
	good := map[string]string{"Cookie": ok["Cookie"], "X-Hyper": "1", "Origin": "http://127.0.0.1:7480"}
	if w := do(h, "POST", "/api/config", "127.0.0.1:7480", good, `{"key":"http_port","value":"8080"}`); w.Code != http.StatusOK {
		t.Errorf("legit POST: got %d: %s", w.Code, w.Body.String())
	}
}

func TestLogNameCannotEscapeLogsFolder(t *testing.T) {
	s := testServer(t)
	h := s.Handler()
	for _, name := range []string{"..%5C..%5Csettings.json", "../x.log", "a%2Fb.log", "hosts"} {
		w := do(h, "GET", "/api/logs?file="+name, "127.0.0.1:7480", cookieFor(s), "")
		if w.Code == http.StatusOK {
			t.Errorf("log name %q must be rejected", name)
		}
	}
}

func TestJobWriteHandlesCarriageReturnAndArabic(t *testing.T) {
	j := &job{status: "running", started: time.Now()}
	io.WriteString(j, "Importing\n  10%\r  50%\r 100%\nمرحبا بالعالم\n")
	got := j.snapshot().Log
	if strings.Contains(got, "10%") || !strings.Contains(got, "100%") {
		t.Errorf("progress should collapse to the last value: %q", got)
	}
	if !strings.Contains(got, "مرحبا بالعالم") {
		t.Errorf("UTF-8 text corrupted: %q", got)
	}
}
