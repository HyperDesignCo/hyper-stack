//go:build darwin && cgo

package ui

import (
	"syscall"

	"github.com/hyperdesign/hyper-stack/internal/config"
	"github.com/hyperdesign/hyper-stack/internal/process"
)

// showWindow asks the running window (another process) to come to the front.
func showWindow(p config.Paths) bool {
	pid := process.PID(spec(p, ""))
	return pid > 0 && syscall.Kill(pid, syscall.SIGUSR1) == nil
}
