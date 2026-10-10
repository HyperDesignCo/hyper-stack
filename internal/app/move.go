package app

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hyperdesign/hyper-stack/internal/config"
)

// moveSkip lists top-level folders that are not copied: they only hold downloads
// that are fetched again if ever needed.
var moveSkip = []string{"cache"}

// rewriteUnder returns p with oldRoot replaced by newRoot when p lies inside
// oldRoot (compared the way Windows does, ignoring case).
func rewriteUnder(p, oldRoot, newRoot string) (string, bool) {
	if p == "" {
		return p, false
	}
	cp, co := filepath.Clean(p), filepath.Clean(oldRoot)
	if strings.EqualFold(cp, co) {
		return filepath.Clean(newRoot), true
	}
	prefix := co + string(filepath.Separator)
	if len(cp) > len(prefix) && strings.EqualFold(cp[:len(prefix)], prefix) {
		return filepath.Join(newRoot, cp[len(prefix):]), true
	}
	return p, false
}

// inside reports whether child is the same folder as parent or lies within it.
func inside(child, parent string) bool {
	_, ok := rewriteUnder(child, parent, "x")
	return ok
}

// rewriteMovedPaths points the project records and the autostart list of the
// (already copied) install at the new folder. Projects outside the install,
// added with "Add folder", keep their own paths.
func (a *App) rewriteMovedPaths(oldRoot, newRoot string) (int, error) {
	changed := 0
	for _, s := range a.sites() {
		root, r1 := rewriteUnder(s.Root, oldRoot, newRoot)
		web, r2 := rewriteUnder(s.WebRoot, oldRoot, newRoot)
		if !r1 && !r2 {
			continue
		}
		s.Root, s.WebRoot = root, web
		if err := a.saveSite(s); err != nil {
			return changed, err
		}
		changed++
	}
	st, err := a.P.LoadSettings()
	if err != nil {
		return changed, err
	}
	for i, p := range st.Autostart {
		if np, ok := rewriteUnder(p, oldRoot, newRoot); ok {
			st.Autostart[i] = np
			changed++
		}
	}
	if err := a.P.SaveSettings(st); err != nil {
		return changed, err
	}
	a.S = st
	return changed, nil
}

// treeStats counts files and bytes under root, skipping the top-level folders in skip.
func treeStats(root string, skip []string) (files int, bytes int64, err error) {
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			for _, s := range skip {
				if strings.EqualFold(rel, s) {
					return filepath.SkipDir
				}
			}
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		files++
		bytes += info.Size()
		return nil
	})
	return files, bytes, err
}

// MoveInstall moves the whole install folder (runtimes, databases, certificates
// and the www folder with every project) to dir. It copies first and leaves the
// old folder alone until the new one has been checked and the projects that were
// running have started from it; if anything fails it switches back and the old
// install is untouched. The old folder is deleted at the end unless keepOld.
func (a *App) MoveInstall(ctx context.Context, dir string, keepOld bool) error {
	if !a.P.Configured {
		return fmt.Errorf("nothing is installed yet. Run: hyper install")
	}
	oldRoot := a.P.Root
	if oldRoot == a.P.Home {
		return fmt.Errorf("this install lives in your Hyper Stack home folder (%s), which cannot be moved. Install into a folder of your choice instead: hyper uninstall --purge, then hyper install --dir <folder>", oldRoot)
	}
	newRoot, err := config.ValidateInstallDir(dir)
	if err != nil {
		return err
	}
	if inside(newRoot, oldRoot) || inside(oldRoot, newRoot) {
		return fmt.Errorf("the new folder and the current one (%s) must be separate folders, not one inside the other", oldRoot)
	}
	if entries, err := os.ReadDir(newRoot); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty. Choose a new or empty folder", newRoot)
	}

	files, size, err := treeStats(oldRoot, moveSkip)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", oldRoot, err)
	}
	a.say("Moving %s to %s (%d files, %.0f MB; the download cache is left behind)", oldRoot, newRoot, files, float64(size)/(1<<20))
	if free, ferr := freeBytes(newRoot); ferr == nil {
		if need := uint64(max(size, 0)) + uint64(max(size, 0))/10 + 256<<20; free < need { //nolint:gosec // size is clamped to be non-negative
			return fmt.Errorf("not enough free space on the new drive: %.1f GB free, about %.1f GB needed", float64(free)/(1<<30), float64(need)/(1<<30))
		}
	}

	// Remember what was running so it can be started again from the new folder.
	var wasOnline []string
	for _, s := range a.State(ctx).Sites {
		if s.Online && s.RootExists {
			wasOnline = append(wasOnline, s.Root)
		}
	}

	a.say("Stopping everything")
	if err := a.Down(ctx, "", true); err != nil {
		return fmt.Errorf("could not stop the services first: %w", err)
	}

	// Nothing may change in the old folder while it is copied, or the check
	// afterwards would fail for no reason: stop writing our own log, and take the
	// baseline now that everything has stopped.
	if a.logFile != nil {
		_ = a.logFile.Close()
		a.logFile = nil
	}
	files, size, err = treeStats(oldRoot, moveSkip)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", oldRoot, err)
	}

	started := time.Now()
	a.say("Copying (this takes a minute or two)")
	if err := copyTree(ctx, oldRoot, newRoot, moveSkip); err != nil {
		return fmt.Errorf("copy failed, nothing was changed: %w", err)
	}
	nf, nb, err := treeStats(newRoot, moveSkip)
	if err != nil {
		return err
	}
	// The copy also gets a marker file and nothing else new, so counts must match.
	if nf != files || nb != size {
		return fmt.Errorf("the copy does not match the original (%d files / %d bytes, expected %d / %d), so nothing was changed. The partial copy is in %s: delete it before trying again", nf, nb, files, size, newRoot)
	}
	a.say("Copied and checked in %s: every file matches", time.Since(started).Round(time.Second))

	// Switch over.
	newP, err := config.ForRoot(newRoot)
	if err != nil {
		return err
	}
	if err := config.SaveRoot(newRoot); err != nil {
		return err
	}
	rollback := func(cause error) error {
		_ = config.SaveRoot(oldRoot)
		return fmt.Errorf("%w. Switched back to %s, which is untouched. The new copy in %s was left for you to look at", cause, oldRoot, newRoot)
	}
	if err := a.retarget(newP); err != nil {
		return rollback(err)
	}
	n, err := a.rewriteMovedPaths(oldRoot, newRoot)
	if err != nil {
		return rollback(err)
	}
	a.say("Pointed %d project/autostart path(s) at the new folder", n)

	// Prove it works before deleting anything: start what was running.
	var failed []string
	for _, root := range wasOnline {
		if np, ok := rewriteUnder(root, oldRoot, newRoot); ok {
			root = np
		}
		if err := a.Up(ctx, root); err != nil {
			a.say("  ! %s: %v", filepath.Base(root), err)
			failed = append(failed, filepath.Base(root))
		}
	}
	if len(failed) > 0 {
		return rollback(fmt.Errorf("%d project(s) did not start from the new folder: %s", len(failed), strings.Join(failed, ", ")))
	}

	if keepOld {
		a.say("Done. The old folder was kept: %s (delete it when you are happy)", oldRoot)
		return nil
	}
	a.say("Everything runs from the new folder. Deleting the old one (%s)", oldRoot)
	if err := removeAllRetry(oldRoot); err != nil {
		a.say("  ! could not delete all of the old folder: %v", err)
		a.say("    It is not used any more; delete %s yourself.", oldRoot)
		return nil
	}
	a.say("Done. Hyper Stack now lives in %s", newRoot)
	return nil
}

func removeAllRetry(dir string) error {
	var err error
	for i := 0; i < 5; i++ {
		if err = os.RemoveAll(dir); err == nil {
			return nil
		}
		time.Sleep(time.Second)
	}
	return err
}
