//go:build darwin

package selfinstall

import (
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// Options select the optional extras.
type Options struct {
	Shortcut  bool
	Autostart bool
}

// Result says what changed.
type Result struct {
	ExePath      string
	Copied       bool
	PathAdded    bool
	ShortcutPath string
	Autostart    bool
}

// rcFiles are the shell startup files that get the PATH block. zsh is the default
// shell on macOS; bash is covered when its file exists.
func rcFiles(home string) []string {
	files := []string{filepath.Join(home, ".zprofile")}
	for _, f := range []string{".bash_profile", ".profile"} {
		if _, err := os.Stat(filepath.Join(home, f)); err == nil {
			files = append(files, filepath.Join(home, f))
		}
	}
	return files
}

func launcherPath(home string) string {
	return filepath.Join(home, "Applications", "Hyper Stack.command")
}

func plistPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", LaunchAgentLabel+".plist")
}

// Install copies the running program into dir, puts dir on PATH (new terminals),
// and applies opts. Safe to run repeatedly.
func Install(dir string, opts Options) (Result, error) {
	var res Result
	home, err := os.UserHomeDir()
	if err != nil {
		return res, err
	}
	src, err := os.Executable()
	if err != nil {
		return res, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	dst := filepath.Join(dir, "hyper")
	res.ExePath = dst
	if !samePath(src, dst) {
		if err := copyExe(src, dst); err != nil {
			return res, err
		}
		res.Copied = true
	}

	for _, f := range rcFiles(home) {
		old, _ := os.ReadFile(f)
		next := AddPathBlock(string(old), dir)
		if next == string(old) {
			continue
		}
		if err := os.WriteFile(f, []byte(next), 0o644); err != nil { //nolint:gosec // the user's own shell startup file
			return res, err
		}
		res.PathAdded = true
	}

	if opts.Shortcut {
		l := launcherPath(home)
		if err := os.MkdirAll(filepath.Dir(l), 0o755); err != nil {
			return res, err
		}
		script := "#!/bin/sh\n# Opens the Hyper Stack dashboard.\nexec " + shDouble(dst) + " ui\n"
		if err := os.WriteFile(l, []byte(script), 0o755); err != nil { //nolint:gosec // a launcher the user double-clicks
			return res, err
		}
		res.ShortcutPath = l
	}
	if opts.Autostart {
		if err := SetAutostart(dst, true); err != nil {
			return res, err
		}
		res.Autostart = true
	}
	return res, nil
}

// Remove undoes the PATH block, the launcher and the login item.
func Remove(dir string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	for _, f := range rcFiles(home) {
		old, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		if next := RemovePathBlock(string(old)); next != string(old) {
			if err := os.WriteFile(f, []byte(next), 0o644); err != nil { //nolint:gosec // the user's own shell startup file
				return err
			}
		}
	}
	if err := os.Remove(launcherPath(home)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return SetAutostart("", false)
}

func samePath(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }

// copyExe replaces dst with src. A running executable can be renamed over on macOS.
func copyExe(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755) //nolint:gosec // our own program file
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// SetAutostart adds or removes the login item (a LaunchAgent).
func SetAutostart(exe string, on bool) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	plist := plistPath(home)
	uid := "0"
	if u, err := user.Current(); err == nil {
		uid = u.Uid
	}
	if !on {
		// Ignore errors: it may simply not be loaded.
		_ = exec.Command("launchctl", "bootout", "gui/"+uid+"/"+LaunchAgentLabel).Run()
		if err := os.Remove(plist); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(plist, []byte(LaunchAgentPlist(exe)), 0o644); err != nil { //nolint:gosec // a per-user LaunchAgent file
		return err
	}
	// It starts at the next login by itself. Loading it now is best effort (there
	// is no login session, for example, on a CI machine).
	_ = exec.Command("launchctl", "bootout", "gui/"+uid+"/"+LaunchAgentLabel).Run()
	if out, err := exec.Command("launchctl", "bootstrap", "gui/"+uid, plist).CombinedOutput(); err != nil {
		_ = out // not fatal
		return nil
	}
	return nil
}

// AutostartEnabled reports whether the login item exists.
func AutostartEnabled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(plistPath(home))
	return err == nil
}
