// Package process starts, stops and inspects long-running background services
// using pid files, so hyper commands stay short-lived.
package process

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Spec describes a background process.
type Spec struct {
	Name    string // used for the pid file and log file
	Bin     string
	Args    []string
	Dir     string
	Env     []string
	RunDir  string // pid files
	LogFile string
}

func (s Spec) pidPath() string { return filepath.Join(s.RunDir, s.Name+".pid") }

// Start launches the process detached. It is a no-op if already running.
func Start(s Spec) error {
	if Running(s) {
		return nil
	}
	if err := os.MkdirAll(s.RunDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.LogFile), 0o755); err != nil {
		return err
	}
	logf, err := os.OpenFile(s.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()

	cmd := exec.Command(s.Bin, s.Args...)
	cmd.Dir = s.Dir
	cmd.Env = append(os.Environ(), s.Env...)
	cmd.Stdout, cmd.Stderr = logf, logf
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot start %s (%s): %w. See %s", s.Name, s.Bin, err, s.LogFile)
	}
	pid := cmd.Process.Pid
	// Reap the child when it exits. Without this a stopped child stays a zombie
	// for as long as this process lives (the dashboard), and kill(pid, 0) keeps
	// reporting it alive on Unix.
	go func() { _ = cmd.Wait() }()
	return os.WriteFile(s.pidPath(), []byte(strconv.Itoa(pid)), 0o644)
}

// PID returns the recorded pid, or 0.
func PID(s Spec) int {
	b, err := os.ReadFile(s.pidPath())
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// Running reports whether the recorded pid is alive.
func Running(s Spec) bool {
	pid := PID(s)
	return pid > 0 && alive(pid)
}

// Stop terminates the process and waits briefly for it to exit.
func Stop(s Spec) error {
	pid := PID(s)
	if pid == 0 || !alive(pid) {
		_ = os.Remove(s.pidPath())
		return nil
	}
	if err := terminate(pid); err != nil {
		return fmt.Errorf("cannot stop %s (pid %d): %w", s.Name, pid, err)
	}
	for i := 0; i < 50; i++ {
		if !alive(pid) {
			_ = os.Remove(s.pidPath())
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("%s (pid %d) did not exit within 5s", s.Name, pid)
}
