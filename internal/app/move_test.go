package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests build their paths with filepath so they mean the same thing on
// Windows (backslashes) and on macOS or Linux (slashes).

func TestRewriteUnder(t *testing.T) {
	base := t.TempDir()
	old := filepath.Join(base, "HyperStack")
	nw := filepath.Join(base, "NewDrive", "HyperStack")

	cases := []struct {
		name, in, want string
		moved          bool
	}{
		{"a project in www", filepath.Join(old, "www", "medical"), filepath.Join(nw, "www", "medical"), true},
		{"the install folder itself", old, nw, true},
		{"a trailing separator", old + string(filepath.Separator), nw, true},
		{"a sibling folder with the same prefix is not inside", old + "Other", old + "Other", false},
		{"a project added from somewhere else stays", filepath.Join(base, "Projects", "shop"), filepath.Join(base, "Projects", "shop"), false},
		{"empty stays empty", "", "", false},
	}
	for _, c := range cases {
		got, moved := rewriteUnder(c.in, old, nw)
		if got != c.want || moved != c.moved {
			t.Errorf("%s: rewriteUnder(%q) = %q,%v want %q,%v", c.name, c.in, got, moved, c.want, c.moved)
		}
	}

	// paths are compared without regard to case, as Windows (and a default Mac) do
	upper := strings.ToUpper(old)
	if got, moved := rewriteUnder(filepath.Join(upper, "www", "blog"), old, nw); !moved || got != filepath.Join(nw, "www", "blog") {
		t.Errorf("a differently-capitalised path must still be recognised: %q %v", got, moved)
	}
	if inside(old+"2", old) || !inside(filepath.Join(old, "www"), old) || !inside(old, old) {
		t.Error("inside() is wrong")
	}
}

// After the copy the records still name the old folder; they must be pointed at
// the new one, but a project added from another place keeps its own path.
func TestRewriteMovedPathsFixesRecordsAndAutostart(t *testing.T) {
	a, _ := newTestApp(t)
	oldRoot := filepath.Join(t.TempDir(), "HyperStack")
	newRoot := a.P.Root
	elsewhere := filepath.Join(t.TempDir(), "Projects", "shop")

	inWWW := Site{Name: "medical", Domain: "medical.test", Root: filepath.Join(oldRoot, "www", "medical"),
		WebRoot: filepath.Join(oldRoot, "www", "medical", "public"), PHP: "7.4", Database: "medical"}
	outside := Site{Name: "shop", Domain: "shop.test", Root: elsewhere,
		WebRoot: filepath.Join(elsewhere, "public"), PHP: "8.3", Database: "shop"}
	for _, s := range []Site{inWWW, outside} {
		if err := a.saveSite(s); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := a.P.LoadSettings()
	st.Autostart = []string{filepath.Join(oldRoot, "www", "medical"), elsewhere}
	if err := a.P.SaveSettings(st); err != nil {
		t.Fatal(err)
	}

	if _, err := a.rewriteMovedPaths(oldRoot, newRoot); err != nil {
		t.Fatal(err)
	}
	m, _ := a.loadSite("medical")
	if m.Root != filepath.Join(newRoot, "www", "medical") || m.WebRoot != filepath.Join(newRoot, "www", "medical", "public") {
		t.Errorf("project in www not moved: %+v", m)
	}
	sh, _ := a.loadSite("shop")
	if sh.Root != elsewhere || sh.WebRoot != filepath.Join(elsewhere, "public") {
		t.Errorf("a project from another place must keep its path: %+v", sh)
	}
	b, err := a.P.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Autostart) != 2 || b.Autostart[0] != filepath.Join(newRoot, "www", "medical") || b.Autostart[1] != elsewhere {
		t.Errorf("autostart list wrong: %v", b.Autostart)
	}
	if m.PHP != "7.4" || m.Database != "medical" {
		t.Errorf("other fields must be kept: %+v", m)
	}
}

func TestTreeStatsSkipsTheCache(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join("bin", "a.exe"), "12345")
	write(filepath.Join("www", "blog", "index.php"), "123")
	write(filepath.Join("cache", "php.zip"), strings.Repeat("x", 1000))
	files, bytes, err := treeStats(root, moveSkip)
	if err != nil {
		t.Fatal(err)
	}
	if files != 2 || bytes != 8 {
		t.Errorf("the cache must not be counted: %d files, %d bytes", files, bytes)
	}
}

func TestMoveRefusesBadTargets(t *testing.T) {
	a, _ := newTestApp(t)
	// the test install lives in its own folder, separate from "home"
	a.P.Home = filepath.Join(filepath.Dir(a.P.Root), "home")
	inner := filepath.Join(a.P.Root, "sub")
	if err := a.MoveInstall(context.Background(), inner, false); err == nil || !strings.Contains(err.Error(), "separate folders") {
		t.Errorf("a folder inside the install must be refused: %v", err)
	}
	if err := a.MoveInstall(context.Background(), filepath.Dir(a.P.Root), false); err == nil {
		t.Error("a folder that contains the install must be refused")
	}
	full := t.TempDir()
	if err := os.WriteFile(filepath.Join(full, "x.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := a.MoveInstall(context.Background(), full, false); err == nil {
		t.Error("a non-empty folder must be refused")
	}
}
