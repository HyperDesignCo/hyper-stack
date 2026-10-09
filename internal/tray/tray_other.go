//go:build !windows

package tray

import "os"
import "os/signal"

// Run has no tray on this platform yet: it just blocks until interrupted.
func Run(open, stopAll func(), onQuit func()) {
	open()
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	<-c
	onQuit()
}

// Stop is a no-op without a tray.
func Stop() {}
