//go:build !windows

package selfinstall

import "fmt"

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
	return Result{}, fmt.Errorf("self-install is only available on Windows in this version")
}

// Remove is only implemented for Windows.
func Remove(dir string) error { return nil }

// AutostartEnabled is always false off Windows.
func AutostartEnabled() bool { return false }
