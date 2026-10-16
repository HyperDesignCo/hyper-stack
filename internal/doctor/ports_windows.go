//go:build windows

package doctor

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// netstatListening lists the TCP sockets (never waits more than 10 seconds).
func netstatListening() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "netstat", "-ano", "-p", "TCP")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.Output()
	return string(out), err
}

// processInfo returns the program name and full path of a process.
func processInfo(pid int) (name, path string) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid)) //nolint:gosec // pids are small
	if err != nil {
		return "", ""
	}
	defer func() { _ = windows.CloseHandle(h) }()
	buf := make([]uint16, 1024)
	n := uint32(len(buf)) //nolint:gosec // fixed small buffer
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &n); err != nil {
		return "", ""
	}
	path = windows.UTF16ToString(buf[:n])
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), path
}
