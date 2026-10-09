//go:build windows

package process

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
	processQueryLimited   = 0x1000
	stillActive           = 259
)

func detach(cmd *exec.Cmd) {
	// Own process group and own hidden console. DETACHED_PROCESS (no console) made
	// console children such as "cmd /C php artisan ..." receive a Ctrl+C when the
	// terminal that launched hyper closed; a private hidden console does not.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup | createNoWindow}
}

func alive(pid int) bool {
	h, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid)) //nolint:gosec // process ids fit in uint32
	if err != nil {
		return false
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// terminate kills the process tree (nginx and php-cgi spawn children).
func terminate(pid int, tree bool) error {
	args := []string{"/PID", strconv.Itoa(pid), "/F"}
	if tree {
		args = append(args, "/T")
	}
	if err := exec.Command("taskkill", args...).Run(); err != nil {
		p, ferr := os.FindProcess(pid)
		if ferr != nil {
			return err
		}
		return p.Kill()
	}
	return nil
}
