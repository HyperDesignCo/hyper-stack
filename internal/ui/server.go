// Package ui serves the local Hyper Stack dashboard on 127.0.0.1.
package ui

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/hyperdesign/hyper-stack/internal/app"
	"github.com/hyperdesign/hyper-stack/internal/config"
)

//go:embed web
var webFS embed.FS

const cookieName = "hyper_stack_ui"

// Factory builds a fresh App writing its output to out.
type Factory func(out io.Writer) (*app.App, error)

// Server is the dashboard.
type Server struct {
	newApp Factory
	token  string
	port   int
	jobs   *jobManager
}

// New creates a Server with a fresh random token.
func New(f Factory) *Server {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return &Server{newApp: f, token: hex.EncodeToString(b), jobs: newJobManager()}
}

// Token is the access token.
func (s *Server) Token() string { return s.token }

// Listen binds 127.0.0.1 on the preferred port, or any free port if taken.
func (s *Server) Listen(preferred int) (net.Listener, error) {
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(preferred))
	if err != nil {
		l, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		return nil, err
	}
	s.port = l.Addr().(*net.TCPAddr).Port
	return l, nil
}

// Port is the bound port.
func (s *Server) Port() int { return s.port }

// URL is the address that logs the browser in (sets the cookie, then redirects).
func (s *Server) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/?t=%s", s.port, s.token)
}

// Handler returns the HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	static := http.FileServer(http.FS(sub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if t := r.URL.Query().Get("t"); t != "" {
			if subtle.ConstantTimeCompare([]byte(t), []byte(s.token)) == 1 {
				http.SetCookie(w, &http.Cookie{Name: cookieName, Value: s.token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode}) //nolint:gosec // plain http on 127.0.0.1, where a Secure cookie would never be sent
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
			http.Error(w, "invalid token", http.StatusForbidden)
			return
		}
		if !s.authed(r) {
			http.Error(w, "Open the dashboard from the Hyper Stack tray icon or run: hyper ui", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; frame-ancestors 'none'")
		static.ServeHTTP(w, r)
	})
	s.routes(mux)
	return s.guard(mux)
}

func (s *Server) authed(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(s.token)) == 1
}

// guard blocks DNS-rebinding (Host check) and cross-site calls (Origin and
// custom-header checks) before anything reaches a handler.
func (s *Server) guard(next http.Handler) http.Handler {
	hostOK := map[string]bool{
		fmt.Sprintf("127.0.0.1:%d", s.port): true,
		fmt.Sprintf("localhost:%d", s.port): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostOK[r.Host] {
			http.Error(w, "bad host", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if !s.authed(r) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if r.Method != http.MethodGet {
				if o := r.Header.Get("Origin"); o != "" {
					u, err := url.Parse(o)
					if err != nil || !hostOK[u.Host] {
						http.Error(w, "bad origin", http.StatusForbidden)
						return
					}
				}
				if r.Header.Get("X-Hyper") != "1" {
					http.Error(w, "missing X-Hyper header", http.StatusForbidden)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) routes(mux *http.ServeMux) {
	mux.HandleFunc("/api/state", s.read(func(a *app.App, r *http.Request) (any, error) {
		return a.State(r.Context()), nil
	}))
	mux.HandleFunc("/api/doctor", s.read(func(a *app.App, r *http.Request) (any, error) {
		return a.Checks(r.Context()), nil
	}))
	mux.HandleFunc("/api/logs", s.read(func(a *app.App, r *http.Request) (any, error) {
		txt, err := a.TailLog(r.URL.Query().Get("file"), 300)
		return map[string]string{"text": txt}, err
	}))
	mux.HandleFunc("/api/job", func(w http.ResponseWriter, r *http.Request) {
		j, ok := s.jobs.get(r.URL.Query().Get("id"))
		if !ok {
			writeErr(w, http.StatusNotFound, fmt.Errorf("unknown job"))
			return
		}
		writeJSON(w, j.snapshot())
	})
	mux.HandleFunc("/api/jobs/current", func(w http.ResponseWriter, r *http.Request) {
		if j := s.jobs.current(); j != nil {
			writeJSON(w, j.snapshot())
			return
		}
		writeJSON(w, nil)
	})

	mux.HandleFunc("/api/install", s.post(func(w http.ResponseWriter, r *http.Request, a *app.App) {
		var in struct {
			Dir                string
			HTTP, HTTPS, MySQL int
		}
		if !decode(w, r, &in) {
			return
		}
		if !a.P.Configured {
			abs, err := config.ValidateInstallDir(in.Dir)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err)
				return
			}
			in.Dir = abs
		}
		s.startJob(w, "Install Hyper Stack", func(ctx context.Context, a *app.App) error {
			return a.InstallTo(ctx, in.Dir, app.Ports{HTTP: in.HTTP, HTTPS: in.HTTPS, MySQL: in.MySQL})
		})
	}))
	mux.HandleFunc("/api/autostart", s.post(func(w http.ResponseWriter, r *http.Request, a *app.App) {
		var in struct{ Enabled bool }
		if !decode(w, r, &in) {
			return
		}
		if err := a.SetLoginAutostart(in.Enabled); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]bool{"enabled": a.LoginAutostart()})
	}))
	mux.HandleFunc("/api/project-autostart", s.post(func(w http.ResponseWriter, r *http.Request, a *app.App) {
		var in struct {
			Path    string
			Enabled bool
		}
		if !decode(w, r, &in) {
			return
		}
		if err := a.SetProjectAutostart(in.Path, in.Enabled); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]bool{"enabled": in.Enabled})
	}))
	mux.HandleFunc("/api/open-www", s.post(func(w http.ResponseWriter, r *http.Request, a *app.App) {
		if err := a.OpenWWW(); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("/api/up-all", s.post(func(w http.ResponseWriter, r *http.Request, _ *app.App) {
		s.startJob(w, "Start all projects", func(ctx context.Context, a *app.App) error { return a.UpAll(ctx) })
	}))
	mux.HandleFunc("/api/pick-folder", s.post(func(w http.ResponseWriter, r *http.Request, _ *app.App) {
		p, err := pickFolder(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]string{"path": p})
	}))
	mux.HandleFunc("/api/inspect", s.post(func(w http.ResponseWriter, r *http.Request, a *app.App) {
		var in struct{ Path string }
		if !decode(w, r, &in) {
			return
		}
		ins, err := a.Inspect(in.Path)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, ins)
	}))
	mux.HandleFunc("/api/up", s.post(func(w http.ResponseWriter, r *http.Request, a *app.App) {
		var in struct {
			Path    string
			Name    string
			Confirm bool
		}
		if !decode(w, r, &in) {
			return
		}
		path := in.Path
		if path == "" && in.Name != "" {
			root, ok := a.SiteRoot(in.Name)
			if !ok {
				writeErr(w, http.StatusNotFound, fmt.Errorf("unknown project %q", in.Name))
				return
			}
			path = root
		}
		ins, err := a.Inspect(path)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if ins.NeedsConfirm && !in.Confirm {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(ins) // the UI shows the setup commands and asks again with confirm=true
			return
		}
		s.startJob(w, "Start "+ins.Name, func(ctx context.Context, a *app.App) error { return a.Up(ctx, ins.Root) })
	}))
	mux.HandleFunc("/api/stop", s.post(func(w http.ResponseWriter, r *http.Request, _ *app.App) {
		var in struct{ Name string }
		if !decode(w, r, &in) {
			return
		}
		s.startJob(w, "Stop "+in.Name, func(ctx context.Context, a *app.App) error { return a.StopSite(ctx, in.Name) })
	}))
	mux.HandleFunc("/api/stop-all", s.post(func(w http.ResponseWriter, r *http.Request, _ *app.App) {
		s.startJob(w, "Stop all services", func(ctx context.Context, a *app.App) error { return a.Down(ctx, "", true) })
	}))
	mux.HandleFunc("/api/forget", s.post(func(w http.ResponseWriter, r *http.Request, _ *app.App) {
		var in struct{ Name string }
		if !decode(w, r, &in) {
			return
		}
		s.startJob(w, "Remove "+in.Name, func(ctx context.Context, a *app.App) error { return a.ForgetSite(ctx, in.Name) })
	}))
	mux.HandleFunc("/api/db-open", s.post(func(w http.ResponseWriter, r *http.Request, a *app.App) {
		var in struct{ Name string }
		if !decode(w, r, &in) {
			return
		}
		root, ok := a.SiteRoot(in.Name)
		if !ok {
			writeErr(w, http.StatusNotFound, fmt.Errorf("unknown project %q", in.Name))
			return
		}
		s.startJob(w, "Open phpMyAdmin for "+in.Name, func(ctx context.Context, a *app.App) error { return a.DBOpen(ctx, root) })
	}))
	mux.HandleFunc("/api/import", s.post(func(w http.ResponseWriter, r *http.Request, a *app.App) {
		var in struct {
			Name, Path string
			Fresh      bool
		}
		if !decode(w, r, &in) {
			return
		}
		root, ok := a.SiteRoot(in.Name)
		if !ok {
			writeErr(w, http.StatusNotFound, fmt.Errorf("unknown project %q", in.Name))
			return
		}
		low := strings.ToLower(in.Path)
		if !strings.HasSuffix(low, ".sql") && !strings.HasSuffix(low, ".sql.gz") && !strings.HasSuffix(low, ".gz") {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("choose a .sql or .sql.gz file"))
			return
		}
		if st, err := os.Stat(in.Path); err != nil || st.IsDir() {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("file not found: %s", in.Path))
			return
		}
		s.startJob(w, "Import "+filepath.Base(in.Path)+" into "+in.Name, func(ctx context.Context, a *app.App) error {
			return a.DBImport(ctx, root, in.Path, in.Fresh)
		})
	}))
	mux.HandleFunc("/api/pick-file", s.post(func(w http.ResponseWriter, r *http.Request, _ *app.App) {
		p, err := pickFile(r.Context())
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, map[string]string{"path": p})
	}))
	mux.HandleFunc("/api/doctor-fix", s.post(func(w http.ResponseWriter, r *http.Request, _ *app.App) {
		s.startJob(w, "Apply safe fixes", func(ctx context.Context, a *app.App) error { return a.Doctor(ctx, true) })
	}))
	mux.HandleFunc("/api/config", s.post(func(w http.ResponseWriter, r *http.Request, a *app.App) {
		var in struct{ Key, Value string }
		if !decode(w, r, &in) {
			return
		}
		if err := a.SetConfig(in.Key, in.Value); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
	}))
}

// read wraps a GET handler that needs an App.
func (s *Server) read(f func(*app.App, *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, err := s.newApp(io.Discard)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		defer a.Close()
		v, err := f(a, r)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, v)
	}
}

// post wraps a POST handler that needs an App for validation.
func (s *Server) post(f func(http.ResponseWriter, *http.Request, *app.App)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		a, err := s.newApp(io.Discard)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		defer a.Close()
		f(w, r, a)
	}
}

func (s *Server) startJob(w http.ResponseWriter, title string, run func(context.Context, *app.App) error) {
	j, err := s.jobs.start(title, func(ctx context.Context, out io.Writer) error {
		a, err := s.newApp(out)
		if err != nil {
			return err
		}
		defer a.Close()
		return run(ctx, a)
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, map[string]string{"job": j.ID})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request: %w", err))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

// OpenBrowser opens url in the default browser.
func OpenBrowser(u string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
	}
	return exec.Command("open", u).Start()
}
