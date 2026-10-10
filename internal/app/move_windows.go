//go:build windows

package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// copyTree copies src to dst with robocopy (built into Windows): fast, many
// threads, long paths, keeps timestamps. skip names top-level folders to leave out.
func copyTree(ctx context.Context, src, dst string, skip []string) error {
	args := []string{src, dst, "/E", "/COPY:DAT", "/DCOPY:DAT", "/R:2", "/W:1", "/MT:16", "/NP", "/NFL", "/NDL", "/NJH"}
	if len(skip) > 0 {
		args = append(args, "/XD")
		for _, s := range skip {
			args = append(args, filepath.Join(src, s))
		}
	}
	out, err := exec.CommandContext(ctx, "robocopy", args...).CombinedOutput() //nolint:gosec // our own folders
	// robocopy exits 0-7 on success (bit flags: files copied, extra files...), 8 and up on failure.
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() < 8 {
		return nil
	}
	if err != nil {
		return fmt.Errorf("robocopy: %w\n%s", err, tailText(string(out), 600))
	}
	return nil
}

// freeBytes returns the free space of the drive that will hold dir.
func freeBytes(dir string) (uint64, error) {
	p := dir
	for {
		if _, err := os.Stat(p); err == nil {
			break
		}
		parent := filepath.Dir(p)
		if parent == p {
			return 0, fmt.Errorf("no existing parent for %s", dir)
		}
		p = parent
	}
	ptr, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return 0, err
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &avail, &total, &free); err != nil {
		return 0, err
	}
	return avail, nil
}

func tailText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}
