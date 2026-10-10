//go:build !windows

package tray

import (
	"os"
	"os/signal"
	"syscall"
)

// Run has no menu-bar icon on this platform yet: it keeps the dashboard serving
// until it is told to stop. (The browser is opened by the caller, when asked to.)
func Run(open, stopAll func(), onQuit func()) {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	onQuit()
}

// Stop is a no-op without a tray.
func Stop() {}
