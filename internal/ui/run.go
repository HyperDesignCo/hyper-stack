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
	"github.com/hyperdesign/hyper-stack/internal/tray"
)

// PreferredPort is the dashboard's usual port, so the address is stable.
const PreferredPort = 7480

type instance struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

func instanceFile(p config.Paths) string { return filepath.Join(p.Run, "ui.json") }

func spec(p config.Paths, exe string) process.Spec {
	return process.Spec{Name: "ui", Bin: exe, Args: []string{"ui", "--serve"}, RunDir: p.Run, LogFile: filepath.Join(p.Logs, "ui.log"), NoTree: true}
}

// Launch makes sure the dashboard is running in the background and opens it.
// It returns quickly; the dashboard keeps running after this process exits.
func Launch(p config.Paths, exe string, openBrowser bool) (string, error) {
	if err := p.Ensure(); err != nil {
		return "", err
	}
	sp := spec(p, exe)
	if process.Running(sp) {
		if in, ok := readInstance(p); ok {
			u := fmt.Sprintf("http://127.0.0.1:%d/?t=%s", in.Port, in.Token)
			if openBrowser {
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
func Serve(p config.Paths, openBrowser bool) error {
	if err := p.Ensure(); err != nil {
		return err
	}
	srv := New(func(out io.Writer) (*app.App, error) {
		return app.NewAt(p, out, false)
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

	open := func() { _ = OpenBrowser(srv.URL()) }
	stopAll := func() {
		_, _ = srv.jobs.start("Stop all services", func(ctx context.Context, out io.Writer) error {
			a, err := app.NewAt(p, out, false)
			if err != nil {
				return err
			}
			defer a.Close()
			return a.Down(ctx, "", true)
		})
	}
	if openBrowser {
		open()
	}
	tray.Run(open, stopAll, func() { _ = httpSrv.Close() })
	return nil
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
