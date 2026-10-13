//go:build windows || (darwin && cgo)

package ui

import (
	"context"
	"io"

	"github.com/hyperdesign/hyper-stack/internal/app"
	"github.com/hyperdesign/hyper-stack/internal/gui"
)

type guiJobs struct{ m *jobManager }

func (g guiJobs) Start(title string, fn func(ctx context.Context, out io.Writer) error) error {
	_, err := g.m.start(title, fn)
	return err
}

func (g guiJobs) Cancel() bool { return g.m.cancelCurrent() }

func (g guiJobs) Snapshot() (title, status, errText, log string) {
	j := g.m.current()
	if j == nil {
		return "", "", "", ""
	}
	s := j.snapshot()
	return s.Title, s.Status, s.Error, s.Log
}

// runShell shows the native window (with its tray icon) until the user quits.
func runShell(srv *Server, show bool, quit func()) error {
	return gui.Run(gui.Backend{
		NewApp:  func(out io.Writer) (*app.App, error) { return app.New(out, false) },
		Jobs:    guiJobs{srv.jobs},
		OpenURL: OpenBrowser,
		WebURL:  srv.URL,
	}, show, quit)
}
