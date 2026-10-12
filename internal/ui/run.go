package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/hyperdesign/hyper-stack/internal/app"
	"github.com/hyperdesign/hyper-stack/internal/config"
	"github.com/hyperdesign/hyper-stack/internal/process"
)

// PreferredPort is the dashboard's usual port, so the address is stable.
const PreferredPort = 7480

type instance struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

func instanceFile(p config.Paths) string { return filepath.Join(p.HomeRun, "ui.json") }

func spec(p config.Paths, exe string) process.Spec {
	return process.Spec{Name: "ui", Bin: exe, Args: []string{"ui", "--serve"}, Dir: p.Home, RunDir: p.HomeRun, LogFile: filepath.Join(p.HomeLogs, "ui.log"), NoTree: true}
}

// Launch makes sure the dashboard is running in the background and opens it.
// It returns quickly; the dashboard keeps running after this process exits.
func Launch(p config.Paths, exe string, openBrowser, startup bool) (string, error) {
	if err := ensureHome(p); err != nil {
		return "", err
	}
	sp := spec(p, exe)
	if process.Running(sp) {
		if in, ok := readInstance(p); ok {
			u := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", in.Port, in.Token)
			if openBrowser && !showWindow(p) {
				_ = OpenBrowser(u)
			}
			return u, nil
		}
		_ = process.Stop(sp) // running but unusable: restart it
	}
	_ = os.Remove(instanceFile(p))
	if !openBrowser {
		sp.Args = append(sp.Args, "--no-browser")
	}
	if startup {
		sp.Args = append(sp.Args, "--startup") // started by the Windows login entry
	}
	if err := process.Start(sp); err != nil {
		return "", err
	}
	for i := 0; i < 50; i++ {
		if in, ok := readInstance(p); ok {
			return fmt.Sprintf("http://127.0.0.1:%d/?t=%s", in.Port, in.Token), nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return "", fmt.Errorf("the dashboard did not start; see %s", sp.LogFile)
}

func readInstance(p config.Paths) (instance, bool) {
	b, err := os.ReadFile(instanceFile(p))
	if err != nil {
		return instance{}, false
	}
	var in instance
	if json.Unmarshal(b, &in) != nil || in.Port == 0 {
		return in, false
	}
	return in, true
}

// Serve runs the dashboard and the tray icon in this process until the user quits.
func Serve(p config.Paths, openBrowser, startup bool) error {
	if err := ensureHome(p); err != nil {
		return err
	}
	// Re-read the install location on every request: the install wizard can
	// choose the folder while the dashboard is already running.
	srv := New(func(out io.Writer) (*app.App, error) {
		return app.New(out, false)
	})
	l, err := srv.Listen(PreferredPort)
	if err != nil {
		return err
	}
	httpSrv := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = httpSrv.Serve(l) }()

	b, _ := json.Marshal(instance{Port: srv.Port(), Token: srv.Token()})
	if err := os.WriteFile(instanceFile(p), b, 0o600); err != nil {
		return err
	}
	defer func() { _ = os.Remove(instanceFile(p)) }()

	if startup {
		// Signed in to Windows: bring up the projects flagged to start automatically.
		_, _ = srv.jobs.start("Start my autostart projects", func(ctx context.Context, out io.Writer) error {
			a, err := app.New(out, false)
			if err != nil {
				return err
			}
			defer a.Close()
			if !a.P.Configured {
				return nil
			}
			return a.UpAutostart(ctx)
		})
	}
	return runShell(srv, openBrowser, func() { _ = httpSrv.Close() })
}

// Stop stops the background dashboard if it is running and reports whether it was.
func Stop(p config.Paths, exe string) bool {
	sp := spec(p, exe)
	if !process.Running(sp) {
		return false
	}
	_ = process.Stop(sp)
	_ = os.Remove(instanceFile(p))
	return true
}

// ensureHome creates only the fixed per-user folders, never an install folder.
func ensureHome(p config.Paths) error {
	for _, d := range []string{p.Home, p.HomeRun, p.HomeLogs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
