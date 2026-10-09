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

func terminate(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}
