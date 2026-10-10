package app

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperdesign/hyper-stack/internal/config"
)

// newTestApp builds an App on a throwaway install folder.
func newTestApp(t *testing.T) (*App, *strings.Builder) {
	t.Helper()
	var out strings.Builder
	p := config.PathsFor(filepath.Join(t.TempDir(), "stack"))
	a, err := NewAt(p, &out, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a, &out
}

func mkProject(t *testing.T, a *App, name, hyperJSON string) string {
	t.Helper()
	dir := filepath.Join(a.P.WWW, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.php"), []byte("<?php"), 0o644); err != nil {
		t.Fatal(err)
	}
	if hyperJSON != "" {
		if err := os.WriteFile(filepath.Join(dir, "hyper.json"), []byte(hyperJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestProjectAutostartRoundTrip(t *testing.T) {
	a, _ := newTestApp(t)
	blog := mkProject(t, a, "blog", "")
	shop := mkProject(t, a, "shop", "")

	// by name, by path, with different capitalisation: always the same project
	if err := a.SetProjectAutostart("blog", true); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProjectAutostart(strings.ToUpper(blog), true); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProjectAutostart(shop, true); err != nil {
		t.Fatal(err)
	}
	if got := a.AutostartProjects(); len(got) != 2 {
		t.Fatalf("blog must be listed once, got %v", got)
	}
	if !a.IsProjectAutostart(blog) || !a.IsProjectAutostart(strings.ToLower(shop)) {
		t.Fatal("flag not found")
	}

	// it survives a restart: a fresh App reads it from settings.json
	b, err := NewAt(a.P, io.Discard, false)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if !b.IsProjectAutostart(blog) || !b.IsProjectAutostart(shop) {
		t.Fatalf("not persisted: %v", b.S.Autostart)
	}

	if err := a.SetProjectAutostart("blog", false); err != nil {
		t.Fatal(err)
	}
	if a.IsProjectAutostart(blog) || !a.IsProjectAutostart(shop) {
		t.Fatalf("remove must only remove blog: %v", a.AutostartProjects())
	}
	if err := a.SetProjectAutostart("blog", false); err != nil {
		t.Fatalf("removing twice must be fine: %v", err)
	}
}

func TestProjectAutostartRefusesWhatCannotStart(t *testing.T) {
	a, _ := newTestApp(t)
	if err := a.SetProjectAutostart(filepath.Join(a.P.WWW, "nope"), true); err == nil {
		t.Error("a folder that does not exist must be refused")
	}
	bad := mkProject(t, a, "broken", `{"name":"Not Valid","php":"7.4"}`)
	if err := a.SetProjectAutostart(bad, true); err == nil {
		t.Error("a project whose hyper.json is invalid cannot start, so it must be refused")
	}
	if len(a.AutostartProjects()) != 0 {
		t.Errorf("nothing should have been saved: %v", a.AutostartProjects())
	}
}

func TestProjectAutostartNeedsAnInstall(t *testing.T) {
	var out strings.Builder
	base := t.TempDir()
	p := config.PathsFor(filepath.Join(base, "x"))
	p.Configured = false
	a, err := NewAt(p, &out, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.SetProjectAutostart(base, true); err == nil {
		t.Fatal("before install there is nothing to start")
	}
}

// At sign-in nothing may run that nobody has reviewed, and a vanished folder
// must not stop the others.
func TestUpAutostartSkipsMissingAndUnreviewedWithoutStartingAnything(t *testing.T) {
	a, out := newTestApp(t)

	gone := mkProject(t, a, "gone", "")
	review := mkProject(t, a, "review", `{"name":"review","php":"8.3","database":{"engine":"mysql","name":"review_db"},"setup":["php -v"]}`)
	if err := a.SetProjectAutostart(gone, true); err != nil {
		t.Fatal(err)
	}
	if err := a.SetProjectAutostart(review, true); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}

	if err := a.UpAutostart(context.Background()); err != nil {
		t.Fatalf("skipping is not a failure: %v", err)
	}
	say := out.String()
	if !strings.Contains(say, "no longer exists") || !strings.Contains(say, "first-time setup commands") {
		t.Errorf("skips must be explained: %q", say)
	}
	if _, err := os.Stat(filepath.Join(a.P.Sites, "review.json")); err == nil {
		t.Error("an unreviewed project must not have been registered or started")
	}
}

func TestUpAutostartWithNothingFlagged(t *testing.T) {
	a, out := newTestApp(t)
	if err := a.UpAutostart(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No projects are set to start automatically") {
		t.Errorf("got %q", out.String())
	}
}
