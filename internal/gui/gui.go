// Package gui is the native Hyper Stack window (Windows). It talks to the same
// app code as the command line, so it needs no browser and no web page.
package gui

import (
	"context"
	"io"

	"github.com/hyperdesign/hyper-stack/internal/app"
)

// Jobs runs one long operation at a time and reports on it.
type Jobs interface {
	// Start runs fn in the background. It fails if another operation is running.
	Start(title string, fn func(ctx context.Context, out io.Writer) error) error
	// Snapshot describes the current (or last) operation. status is "", "running", "done" or "failed".
	Snapshot() (title, status, errText, log string)
}

// Backend is what the window needs from the rest of the program.
type Backend struct {
	NewApp  func(out io.Writer) (*app.App, error)
	Jobs    Jobs
	OpenURL func(url string) error
	WebURL  func() string // the browser version of the dashboard
	Version string
}
