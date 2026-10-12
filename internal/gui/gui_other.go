//go:build !windows

package gui

import "errors"

// Run is not available on this platform yet: the browser dashboard is used.
func Run(b Backend, show bool, onQuit func()) error {
	return errors.New("the native window is only available on Windows")
}

// Available reports whether the native window exists on this platform.
func Available() bool { return false }

// Show asks a running window to come to the front.
func Show() bool { return false }
