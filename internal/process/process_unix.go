//go:build !windows

package process

import (
	"os/exec"
	"syscall"
)

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func alive(pid int) bool {
	return syscall.Kill(pid, 0) == nil
}

func terminate(pid int, tree bool) error {
	if !tree {
		return syscall.Kill(pid, syscall.SIGTERM)
	}
	return syscall.Kill(-pid, syscall.SIGTERM)
}
