//go:build windows

package selfinstall

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

const (
	envKey  = `Environment`
	runKey  = `Software\Microsoft\Windows\CurrentVersion\Run`
	runName = "HyperStack"
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

// Install copies the running executable into dir (replacing an older copy),
// adds dir to the user PATH, and applies opts. Safe to run repeatedly.
func Install(dir string, opts Options) (Result, error) {
	var res Result
	src, err := os.Executable()
	if err != nil {
		return res, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	dst := filepath.Join(dir, "hyper.exe")
	res.ExePath = dst

	if !strings.EqualFold(filepath.Clean(src), filepath.Clean(dst)) {
		if err := copyFile(src, dst); err != nil {
			return res, err
		}
		res.Copied = true
	}

	added, err := updateUserPath(func(p string) string { return AddToPath(p, dir) })
	if err != nil {
		return res, err
	}
	res.PathAdded = added

	if opts.Shortcut {
		if res.ShortcutPath, err = createShortcut(dst); err != nil {
			return res, err
		}
	}
	if opts.Autostart {
		if err := SetAutostart(dst, true); err != nil {
			return res, err
		}
		res.Autostart = true
	}
	return res, nil
}

// Remove undoes PATH, shortcut and autostart. The copied exe is left for the caller.
func Remove(dir string) error {
	if _, err := updateUserPath(func(p string) string { return RemoveFromPath(p, dir) }); err != nil {
		return err
	}
	if err := SetAutostart("", false); err != nil {
		return err
	}
	if p := shortcutPath(); p != "" {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	// A running exe cannot be overwritten on Windows but can be renamed away.
	old := dst + ".old"
	_ = os.Remove(old)
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			_ = os.Remove(tmp)
			return fmt.Errorf("cannot replace %s: %w. Close the Hyper Stack dashboard and try again", dst, err)
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Rename(old, dst)
		return err
	}
	_ = os.Remove(old) // fails while the old exe is still running; harmless
	return nil
}

// updateUserPath rewrites HKCU\Environment\Path with f and tells running
// programs. It keeps the value's REG_EXPAND_SZ type and reports whether it changed.
func updateUserPath(f func(string) string) (bool, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, envKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return false, fmt.Errorf("cannot open the user environment: %w", err)
	}
	defer k.Close()
	cur, _, err := k.GetStringValue("Path")
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return false, err
	}
	next := f(cur)
	if next == cur {
		return false, nil
	}
	if err := k.SetExpandStringValue("Path", next); err != nil {
		return false, err
	}
	broadcastEnvChange()
	return true, nil
}

func broadcastEnvChange() {
	user32 := syscall.NewLazyDLL("user32.dll")
	proc := user32.NewProc("SendMessageTimeoutW")
	env, _ := syscall.UTF16PtrFromString("Environment")
	var result uintptr
	// HWND_BROADCAST, WM_SETTINGCHANGE, 0, "Environment", SMTO_ABORTIFHUNG, 5000ms
	_, _, _ = proc.Call(0xffff, 0x001A, 0, uintptr(unsafe.Pointer(env)), 0x0002, 5000, uintptr(unsafe.Pointer(&result))) //nolint:gosec // Win32 SendMessageTimeout needs raw pointers
}

// SetAutostart adds or removes the Windows login entry. At login it starts the
// tray icon and dashboard, which then start the projects flagged for autostart.
func SetAutostart(exe string, on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	return k.SetStringValue(runName, `"`+exe+`" ui --no-browser --startup`)
}

// AutostartEnabled reports whether the login entry exists.
func AutostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runName)
	return err == nil
}

func shortcutPath() string {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return ""
	}
	return filepath.Join(appdata, "Microsoft", "Windows", "Start Menu", "Programs", "Hyper Stack.lnk")
}

func createShortcut(exe string) (string, error) {
	lnk := shortcutPath()
	if lnk == "" {
		return "", fmt.Errorf("APPDATA is not set")
	}
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := fmt.Sprintf(`$s = (New-Object -ComObject WScript.Shell).CreateShortcut(%s); $s.TargetPath = %s; $s.Arguments = 'ui'; `+
		`$s.WorkingDirectory = %s; $s.IconLocation = %s; $s.Description = 'Hyper Stack dashboard'; $s.WindowStyle = 7; $s.Save()`,
		q(lnk), q(exe), q(filepath.Dir(exe)), q(exe+",0"))
	if out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput(); err != nil {
		return "", fmt.Errorf("cannot create the Start menu shortcut: %w\n%s", err, out)
	}
	return lnk, nil
}
