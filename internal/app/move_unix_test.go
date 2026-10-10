//go:build !windows

package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyTreeKeepsModesLinksAndSkipsTheCache(t *testing.T) {
	src, dst := t.TempDir(), filepath.Join(t.TempDir(), "new")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(src, "bin"), 0o755))
	must(os.MkdirAll(filepath.Join(src, "www", "blog"), 0o755))
	must(os.MkdirAll(filepath.Join(src, "cache"), 0o755))
	must(os.WriteFile(filepath.Join(src, "bin", "mysqld"), []byte("binary"), 0o755))
	must(os.WriteFile(filepath.Join(src, "www", "blog", "index.php"), []byte("<?php"), 0o644))
	must(os.WriteFile(filepath.Join(src, "cache", "php.tar.gz"), []byte("download"), 0o644))
	must(os.Symlink("mysqld", filepath.Join(src, "bin", "mysqld-link")))

	must(copyTree(context.Background(), src, dst, moveSkip))

	st, err := os.Stat(filepath.Join(dst, "bin", "mysqld"))
	must(err)
	if st.Mode().Perm()&0o111 == 0 {
		t.Error("a program must stay executable after the copy")
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "www", "blog", "index.php")); string(b) != "<?php" {
		t.Errorf("file content lost: %q", b)
	}
	if l, err := os.Readlink(filepath.Join(dst, "bin", "mysqld-link")); err != nil || l != "mysqld" {
		t.Errorf("a symbolic link must stay a link: %q %v", l, err)
	}
	if _, err := os.Stat(filepath.Join(dst, "cache")); err == nil {
		t.Error("the cache must be left behind")
	}
	// the same count and size the move checks against
	nf, nb, _ := treeStats(dst, nil)
	of, ob, _ := treeStats(src, moveSkip)
	if nf != of || nb != ob {
		t.Errorf("copy differs from the original: %d/%d files, %d/%d bytes", nf, of, nb, ob)
	}
}

func TestFreeBytesWorksForAFolderThatDoesNotExistYet(t *testing.T) {
	free, err := freeBytes(filepath.Join(t.TempDir(), "not", "yet", "there"))
	if err != nil || free == 0 {
		t.Fatalf("free=%d err=%v", free, err)
	}
}
