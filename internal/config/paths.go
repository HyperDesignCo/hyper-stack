// Package config holds Hyper Stack's own locations on disk.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Paths are all directories Hyper Stack writes to.
type Paths struct {
	Root  string // data dir
	Bin   string // downloaded runtimes
	Certs string
	Logs  string
	Run   string // pid files, generated configs
	Sites string // registered projects
}

// DataDir returns the per-user data directory for the current OS.
func DataDir() (string, error) {
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

// PathsFor builds the layout under root.
func PathsFor(root string) Paths {
	return Paths{
		Root:  root,
		Bin:   filepath.Join(root, "bin"),
		Certs: filepath.Join(root, "certs"),
		Logs:  filepath.Join(root, "logs"),
		Run:   filepath.Join(root, "run"),
		Sites: filepath.Join(root, "sites"),
	}
}

// Default returns the layout under DataDir.
func Default() (Paths, error) {
	r, err := DataDir()
	if err != nil {
		return Paths{}, err
	}
	return PathsFor(r), nil
}

// Ensure creates every directory.
func (p Paths) Ensure() error {
	for _, d := range []string{p.Root, p.Bin, p.Certs, p.Logs, p.Run, p.Sites} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("cannot create %s: %w", d, err)
		}
	}
	return nil
}
