//go:build !windows

package selfinstall

import "errors"

var errUnsupported = errors.New("self-install is only available on Windows in this version")

// Options select the optional extras.
type Options struct {
	Shortcut  bool
	Autostart bool
}

// Result says what changed.
type Result struct {
	ExePath      string
	Copied       bool
	PathAdded    bool
	ShortcutPath string
	Autostart    bool
}

// Install is only implemented for Windows.
func Install(dir string, opts Options) (Result, error) {
	return Result{}, errUnsupported
}

// Remove is only implemented for Windows.
func Remove(dir string) error { return nil }

// SetAutostart is only implemented for Windows.
func SetAutostart(exe string, on bool) error { return errUnsupported }

// AutostartEnabled is always false off Windows.
func AutostartEnabled() bool { return false }
