// Package config holds Hyper Stack's own locations on disk.
//
// Two places matter:
//
//   - Home (%LOCALAPPDATA%\HyperStack): fixed. Holds hyper.exe, the dashboard's
//     process files and location.json, the pointer to the install folder.
//   - Root: the install folder the user chose (like C:\laragon). It holds bin\
//     (PHP, MySQL, nginx, phpMyAdmin...), data\ (databases), certs\, logs\ and
//     www\ (all projects). Before an install exists, and for installs made
//     before folders could be chosen, Root is Home itself.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
)

// Paths are all directories Hyper Stack writes to.
type Paths struct {
	Home       string // fixed per-user folder (hyper.exe, location pointer)
	HomeRun    string // dashboard pid/instance files
	HomeLogs   string // dashboard log
	Root       string // the install folder
	Bin        string // downloaded runtimes
	Certs      string
	Logs       string
	Run        string // pid files, generated configs
	Sites      string // registered projects
	WWW        string // projects live here: every subfolder becomes <name>.test
	Configured bool   // an install folder has been chosen (or an older install exists)
}

const (
	pointerFile = "location.json"
	markerFile  = ".hyper-stack"
)

// Home returns the fixed per-user folder. HYPER_HOME overrides it (tests, portable use).
func Home() (string, error) {
	if h := os.Getenv("HYPER_HOME"); h != "" {
		return h, nil
	}
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("LOCALAPPDATA is not set")
		}
		return filepath.Join(base, "HyperStack"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "HyperStack"), nil
	}
	return "", fmt.Errorf("unsupported OS %s: Hyper Stack supports Windows and macOS", runtime.GOOS)
}

// AppDir is where self-install puts hyper.exe. It never moves with the install folder.
func AppDir() (string, error) {
	h, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, "app"), nil
}

// DefaultInstallDir is the suggested install folder (C:\HyperStack on Windows).
func DefaultInstallDir() string {
	if runtime.GOOS == "windows" {
		drive := os.Getenv("SystemDrive")
		if drive == "" {
			drive = "C:"
		}
		return drive + `\HyperStack`
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, "HyperStack")
	}
	return "/opt/hyperstack"
}

// PathsFor builds the layout with root as both Home and Root (tests, legacy).
func PathsFor(root string) Paths { return layout(root, root, true) }

func layout(home, root string, configured bool) Paths {
	return Paths{
		Home: home, HomeRun: filepath.Join(home, "run"), HomeLogs: filepath.Join(home, "logs"),
		Root: root, Bin: filepath.Join(root, "bin"), Certs: filepath.Join(root, "certs"),
		Logs: filepath.Join(root, "logs"), Run: filepath.Join(root, "run"),
		Sites: filepath.Join(root, "sites"), WWW: filepath.Join(root, "www"),
		Configured: configured,
	}
}

type pointer struct {
	Root string `json:"root"`
}

// Default resolves the layout: the folder in location.json if there is one,
// else Home when an older install already lives there, else "not configured".
func Default() (Paths, error) {
	home, err := Home()
	if err != nil {
		return Paths{}, err
	}
	if b, err := os.ReadFile(filepath.Join(home, pointerFile)); err == nil {
		var ptr pointer
		if json.Unmarshal(b, &ptr) == nil && ptr.Root != "" {
			return layout(home, ptr.Root, true), nil
		}
	}
	legacy := false
	if _, err := os.Stat(filepath.Join(home, "bin", "nginx", ".hyper-installed")); err == nil {
		legacy = true
	}
	return layout(home, home, legacy), nil
}

// SaveRoot records the chosen install folder.
func SaveRoot(root string) error {
	home, err := Home()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(pointer{Root: root}, "", "  ")
	return os.WriteFile(filepath.Join(home, pointerFile), b, 0o644)
}

// ClearRoot forgets the install folder (after a purge), so the next install asks again.
func ClearRoot() error {
	home, err := Home()
	if err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(home, pointerFile)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ForRoot returns the layout for a (validated) install folder under the current Home.
func ForRoot(root string) (Paths, error) {
	home, err := Home()
	if err != nil {
		return Paths{}, err
	}
	return layout(home, root, true), nil
}

// ValidateInstallDir checks a folder is a sane place for the stack: absolute,
// plain ASCII (MySQL and nginx mishandle other characters in paths), not a
// system folder, and either empty or already a Hyper Stack folder.
func ValidateInstallDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("choose an install folder")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	for _, r := range abs {
		if r > unicode.MaxASCII {
			return "", fmt.Errorf("the folder path must use plain English letters, digits and symbols (no Arabic or accented characters): %s", abs)
		}
	}
	if vol := filepath.VolumeName(abs); vol != "" && strings.TrimPrefix(abs, vol) == `\` {
		return "", fmt.Errorf("choose a folder, not the root of a drive (for example %s)", DefaultInstallDir())
	}
	lower := strings.ToLower(abs)
	for _, bad := range []string{`\windows`, `\program files`, `\program files (x86)`} {
		if strings.Contains(lower, bad+`\`) || strings.HasSuffix(lower, bad) {
			return "", fmt.Errorf("do not install inside %s (needs administrator rights). Try %s", bad[1:], DefaultInstallDir())
		}
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return abs, nil
		}
		return "", err
	}
	if len(entries) == 0 {
		return abs, nil
	}
	if _, err := os.Stat(filepath.Join(abs, markerFile)); err == nil {
		return abs, nil
	}
	return "", fmt.Errorf("%s is not empty. Choose an empty folder or a new one", abs)
}

// Ensure creates every directory and marks the folder as a Hyper Stack folder.
func (p Paths) Ensure() error {
	for _, d := range []string{p.Home, p.Root, p.Bin, p.Certs, p.Logs, p.Run, p.Sites, p.WWW, p.HomeRun, p.HomeLogs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("cannot create %s: %w", d, err)
		}
	}
	if p.Root != p.Home {
		_ = os.WriteFile(filepath.Join(p.Root, markerFile), []byte("Hyper Stack install folder. Your projects are in www\\.\n"), 0o644)
	}
	return nil
}
