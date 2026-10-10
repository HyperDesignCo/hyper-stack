package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/hyperdesign/hyper-stack/internal/project"
)

// PHPInfo describes which PHP a project runs on and where that came from.
type PHPInfo struct {
	Project   string
	Version   string
	Source    string // "hyper.json" or "detected from composer.json / composer.lock"
	Available []string
}

// ProjectPHP reports the PHP version of a project folder ("" means the current folder).
func (a *App) ProjectPHP(arg string) (PHPInfo, error) {
	dir, err := a.resolveProject(arg)
	if err != nil {
		return PHPInfo{}, err
	}
	cfg, auto, err := project.Resolve(dir)
	if err != nil {
		return PHPInfo{}, err
	}
	src := "hyper.json"
	if auto {
		src = "detected from composer.json and composer.lock"
	}
	return PHPInfo{Project: cfg.Name, Version: cfg.PHP, Source: src, Available: project.SupportedPHP()}, nil
}

// SetProjectPHP changes the PHP version a project runs on and, unless the project
// still has unreviewed first-time setup commands, restarts it on that version.
// The version is written to the project's hyper.json (created if missing); every
// other setting in that file is kept.
func (a *App) SetProjectPHP(ctx context.Context, arg, version string) error {
	if !project.IsSupportedPHP(version) {
		return fmt.Errorf("PHP %s is not available. Choose one of: %s", version, strings.Join(project.SupportedPHP(), ", "))
	}
	dir, err := a.resolveProject(arg)
	if err != nil {
		return err
	}
	if !dirExists(dir) {
		return fmt.Errorf("%s is not a folder", dir)
	}
	if err := writePHPVersion(dir, version); err != nil {
		return err
	}
	a.say("%s now uses PHP %s (saved in hyper.json)", filepath.Base(dir), version)

	ins, err := a.Inspect(dir)
	if err != nil {
		return err
	}
	if ins.NeedsConfirm {
		a.say("It has first-time setup commands, so it was not restarted. Start it from the dashboard (or run hyper up inside it) to review them.")
		return nil
	}
	return a.Up(ctx, dir)
}

// writePHPVersion sets "php" in dir/hyper.json, creating the file from the
// folder's automatic settings when there is none.
func writePHPVersion(dir, version string) error {
	path := filepath.Join(dir, "hyper.json")
	doc := map[string]any{}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
		if err := json.Unmarshal(b, &doc); err != nil {
			return fmt.Errorf("hyper.json is not valid JSON, fix it first: %w", err)
		}
	case os.IsNotExist(err):
		cfg, aerr := project.Auto(dir)
		if aerr != nil {
			return aerr
		}
		doc["name"] = cfg.Name
		doc["database"] = map[string]any{"engine": "mysql", "name": cfg.Database.Name}
	default:
		return err
	}
	doc["php"] = version
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644) //nolint:gosec // the project's own hyper.json
}

// Say prints a line to the app's output (for the command layer).
func (a *App) Say(format string, args ...any) { a.say(format, args...) }
