//go:build windows

package process

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
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

var queryImageName = syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")

// isProgram reports whether pid runs the program bin. Windows reuses process ids,
// so a stale pid file can point at an unrelated process. When the image cannot be
// read the answer is "yes", as before.
func isProgram(pid int, bin string) bool {
	if bin == "" {
		return true
	}
	h, err := syscall.OpenProcess(processQueryLimited, false, uint32(pid)) //nolint:gosec // process ids fit in uint32
	if err != nil {
		return true
	}
	defer func() { _ = syscall.CloseHandle(h) }()
	buf := make([]uint16, 1024)
	n := uint32(len(buf))
	r, _, _ := queryImageName.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))) //nolint:gosec // Windows API call
	if r == 0 {
		return true
	}
	return strings.EqualFold(filepath.Base(syscall.UTF16ToString(buf[:n])), filepath.Base(bin))
}
