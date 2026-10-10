package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRewriteUnder(t *testing.T) {
	old, nw := `C:\HyperStack`, `E:\HyperStack`
	cases := []struct {
		in, want string
		moved    bool
	}{
		{`C:\HyperStack\www\medical`, `E:\HyperStack\www\medical`, true},
		{`c:\hyperstack\WWW\blog`, `E:\HyperStack\WWW\blog`, true}, // Windows ignores case
		{`C:\HyperStack`, `E:\HyperStack`, true},
		{`C:\HyperStack\`, `E:\HyperStack`, true},
		{`C:\HyperStackOther\www\x`, `C:\HyperStackOther\www\x`, false}, // a sibling, not inside
		{`D:\Projects\shop`, `D:\Projects\shop`, false},                 // added from elsewhere: stays
		{``, ``, false},
	}
	for _, c := range cases {
		got, moved := rewriteUnder(c.in, old, nw)
		if got != c.want || moved != c.moved {
			t.Errorf("rewriteUnder(%q) = %q,%v want %q,%v", c.in, got, moved, c.want, c.moved)
		}
	}
	if inside(`C:\HyperStack2`, old) || !inside(`C:\HyperStack\www`, old) || !inside(old, old) {
		t.Error("inside() is wrong")
	}
}

// After the copy the records still say "C:\..."; they must be pointed at the new
// folder, but a project added from another drive must keep its own path.
func TestRewriteMovedPathsFixesRecordsAndAutostart(t *testing.T) {
	a, _ := newTestApp(t)
	oldRoot := `C:\HyperStack`
	newRoot := a.P.Root
	inWWW := Site{Name: "medical", Domain: "medical.test", Root: oldRoot + `\www\medical`, WebRoot: oldRoot + `\www\medical\public`, PHP: "7.4", Database: "medical"}
	outside := Site{Name: "shop", Domain: "shop.test", Root: `D:\Projects\shop`, WebRoot: `D:\Projects\shop\public`, PHP: "8.3", Database: "shop"}
	for _, s := range []Site{inWWW, outside} {
		if err := a.saveSite(s); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := a.P.LoadSettings()
	st.Autostart = []string{oldRoot + `\www\medical`, `D:\Projects\shop`}
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
	if sh.Root != `D:\Projects\shop` || sh.WebRoot != `D:\Projects\shop\public` {
		t.Errorf("a project from another drive must keep its path: %+v", sh)
	}
	b, err := a.P.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Autostart) != 2 || b.Autostart[0] != filepath.Join(newRoot, "www", "medical") || b.Autostart[1] != `D:\Projects\shop` {
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
	write("bin/a.exe", "12345")
	write("www/blog/index.php", "123")
	write("cache/php.zip", strings.Repeat("x", 1000))
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
