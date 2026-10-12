//go:build !windows && !(darwin && cgo)

package ui

import (
	"context"
	"io"

	"github.com/hyperdesign/hyper-stack/internal/app"
	"github.com/hyperdesign/hyper-stack/internal/config"
	"github.com/hyperdesign/hyper-stack/internal/tray"
)

// runShell keeps the dashboard serving (there is no native window here yet).
func runShell(srv *Server, show bool, quit func()) error {
	if show {
		_ = OpenBrowser(srv.URL())
	}
	tray.Run(func() { _ = OpenBrowser(srv.URL()) }, func() {
		_, _ = srv.jobs.start("Stop all services", func(ctx context.Context, out io.Writer) error { return stopAllServices(ctx, out) })
	}, quit)
	return nil
}

// showWindow has nothing to show off Windows.
func showWindow(config.Paths) bool { return false }

// stopAllServices stops the web server, PHP and MySQL.
func stopAllServices(ctx context.Context, out io.Writer) error {
	a, err := app.New(out, false)
	if err != nil {
		return err
	}
	defer a.Close()
	return a.Down(ctx, "", true)
}
