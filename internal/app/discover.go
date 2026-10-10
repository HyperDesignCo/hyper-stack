package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hyperdesign/hyper-stack/internal/project"
)

// Discovered is a folder in www that is not started yet.
type Discovered struct {
	Name     string
	Domain   string
	Root     string
	PHP      string
	Database string
	Auto     bool   // configured from the folder, no hyper.json
	Error    string // why it cannot be started, if so
}

// wwwFolders lists the project folders directly inside www.
func (a *App) wwwFolders() []string {
	entries, err := os.ReadDir(a.P.WWW)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		out = append(out, filepath.Join(a.P.WWW, e.Name()))
	}
	return out
}

// Discover returns the www folders that are not registered projects yet.
func (a *App) Discover() []Discovered {
	taken := map[string]string{} // site name -> folder that owns it
	registered := map[string]bool{}
	for _, s := range a.sites() {
		registered[strings.ToLower(filepath.Clean(s.Root))] = true
		taken[s.Name] = s.Root
	}
	var out []Discovered
	for _, dir := range a.wwwFolders() {
		if registered[strings.ToLower(filepath.Clean(dir))] {
			continue
		}
		d := Discovered{Root: dir, Name: filepath.Base(dir)}
		cfg, auto, err := project.Resolve(dir)
		if err != nil {
			d.Error = firstLine(err.Error())
			out = append(out, d)
			continue
		}
		d.Name, d.Domain, d.PHP, d.Database, d.Auto = cfg.Name, cfg.Domain, cfg.PHP, cfg.Database.Name, auto
		if owner, clash := taken[cfg.Name]; clash && !strings.EqualFold(filepath.Clean(owner), filepath.Clean(dir)) {
			d.Name = filepath.Base(dir)
			d.Error = fmt.Sprintf("same site name as %s. Rename one of the two folders", filepath.Base(owner))
		} else {
			taken[cfg.Name] = dir
		}
		out = append(out, d)
	}
	return out
}

// UpAll starts every project in www. One failing project does not stop the rest.
func (a *App) UpAll(ctx context.Context) error {
	dirs := a.wwwFolders()
	if len(dirs) == 0 {
		a.say("No projects yet. Create a folder inside %s (for example %s) and run this again.", a.P.WWW, filepath.Join(a.P.WWW, "myapp"))
		return nil
	}
	var failed []string
	for _, dir := range dirs {
		// First-time setup commands come from the project's own hyper.json. "All"
		// never runs unreviewed commands: start those one by one.
		if ins, err := a.Inspect(dir); err == nil && ins.NeedsConfirm {
			a.say("  - %s has first-time setup commands. Start it on its own to review them (cd into it and run hyper up, or use Start in the dashboard)", filepath.Base(dir))
			continue
		}
		if err := a.Up(ctx, dir); err != nil {
			a.say("  ! %s: %v", filepath.Base(dir), err)
			failed = append(failed, filepath.Base(dir))
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d project(s) could not start: %s", len(failed), len(dirs), strings.Join(failed, ", "))
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// OpenWWW opens the www folder in the file manager.
func (a *App) OpenWWW() error {
	if !a.P.Configured {
		return fmt.Errorf("nothing is installed yet. Run: hyper install")
	}
	if err := os.MkdirAll(a.P.WWW, 0o755); err != nil {
		return err
	}
	a.say("%s", a.P.WWW)
	return openFolder(a.P.WWW)
}

func openFolder(dir string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("explorer", dir).Start()
	}
	return exec.Command("open", dir).Start()
}
