package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hyperdesign/hyper-stack/internal/config"
	"github.com/hyperdesign/hyper-stack/internal/project"
	"github.com/hyperdesign/hyper-stack/internal/selfinstall"
)

// LoginAutostart reports whether Hyper Stack starts when you sign in to Windows.
func (a *App) LoginAutostart() bool { return selfinstall.AutostartEnabled() }

// SetLoginAutostart turns "start Hyper Stack when I sign in" on or off. At login
// the tray icon and dashboard start, and they start the projects flagged with
// SetProjectAutostart.
func (a *App) SetLoginAutostart(on bool) error {
	dir, err := config.AppDir()
	if err != nil {
		return err
	}
	exe := filepath.Join(dir, config.ExeName())
	if on {
		if _, err := os.Stat(exe); err != nil {
			return fmt.Errorf("hyper is not installed for your user yet. Run: hyper self-install --autostart")
		}
	}
	return selfinstall.SetAutostart(exe, on)
}

// samePath compares two folders the way Windows does.
func samePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// resolveProject turns what the user typed into a project folder: a path, the
// name of a project in www or of a registered project, or "" for the current folder.
func (a *App) resolveProject(arg string) (string, error) {
	arg = strings.Trim(strings.TrimSpace(arg), `"`)
	if arg == "" {
		return os.Getwd()
	}
	if filepath.IsAbs(arg) || strings.ContainsAny(arg, `/\`) {
		return filepath.Abs(arg)
	}
	if s, ok := a.loadSite(arg); ok {
		return s.Root, nil
	}
	if p := filepath.Join(a.P.WWW, arg); dirExists(p) {
		return p, nil
	}
	return filepath.Abs(arg)
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// AutostartProjects lists the project folders flagged to start at login.
func (a *App) AutostartProjects() []string {
	out := append([]string(nil), a.S.Autostart...)
	sort.Strings(out)
	return out
}

// IsProjectAutostart reports whether a project folder is flagged.
func (a *App) IsProjectAutostart(root string) bool {
	for _, p := range a.S.Autostart {
		if samePath(p, root) {
			return true
		}
	}
	return false
}

// SetProjectAutostart flags (or unflags) a project to start whenever Hyper Stack
// starts at login. arg is a folder, a project name, or "" for the current folder.
func (a *App) SetProjectAutostart(arg string, on bool) error {
	if !a.P.Configured {
		return fmt.Errorf("nothing is installed yet. Run: hyper install")
	}
	root, err := a.resolveProject(arg)
	if err != nil {
		return err
	}
	if on {
		if !dirExists(root) {
			return fmt.Errorf("%s is not a folder", root)
		}
		if _, _, err := project.Resolve(root); err != nil {
			return fmt.Errorf("this folder cannot be started, so it cannot start at login: %w", err)
		}
	}
	// Work on the saved settings, not a copy that may be stale.
	s, err := a.P.LoadSettings()
	if err != nil {
		return err
	}
	var kept []string
	for _, p := range s.Autostart {
		if !samePath(p, root) {
			kept = append(kept, p)
		}
	}
	if on {
		kept = append(kept, filepath.Clean(root))
	}
	sort.Strings(kept)
	s.Autostart = kept
	if err := a.P.SaveSettings(s); err != nil {
		return err
	}
	a.S.Autostart = kept
	return nil
}

// UpAutostart starts every project flagged for autostart. It is what runs when
// Hyper Stack starts at login. Like "start all", it never runs first-time setup
// commands nobody has reviewed.
func (a *App) UpAutostart(ctx context.Context) error {
	list := a.AutostartProjects()
	if len(list) == 0 {
		a.say("No projects are set to start automatically.")
		return nil
	}
	var failed []string
	for _, dir := range list {
		name := filepath.Base(dir)
		if !dirExists(dir) {
			a.say("  - %s: the folder no longer exists, skipped (%s)", name, dir)
			continue
		}
		if ins, err := a.Inspect(dir); err == nil && ins.NeedsConfirm {
			a.say("  - %s has first-time setup commands. Start it once yourself to review them; after that it can start at login.", name)
			continue
		}
		if err := a.Up(ctx, dir); err != nil {
			a.say("  ! %s: %v", name, err)
			failed = append(failed, name)
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("%d of %d autostart project(s) could not start: %s", len(failed), len(list), strings.Join(failed, ", "))
	}
	return nil
}

// AutostartStatus prints the login setting and the flagged projects.
func (a *App) AutostartStatus() {
	if a.LoginAutostart() {
		a.say("Start Hyper Stack when I sign in to Windows:  ON")
	} else {
		a.say("Start Hyper Stack when I sign in to Windows:  off  (turn on with: hyper autostart on)")
	}
	list := a.AutostartProjects()
	if len(list) == 0 {
		a.say("Projects that start automatically:            none  (add one with: hyper autostart add <folder>)")
		return
	}
	a.say("Projects that start automatically:")
	for _, p := range list {
		note := ""
		if !dirExists(p) {
			note = "   (folder missing)"
		}
		a.say("  %s%s", p, note)
	}
}
