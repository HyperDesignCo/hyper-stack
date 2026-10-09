package envfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApplyPreservesEverythingElse(t *testing.T) {
	in := "# comment\nAPP_NAME=Foo\nDB_HOST=old\n\nSECRET=abc # keep\n"
	out, err := Apply(in, map[string]string{"DB_HOST": "127.0.0.1", "DB_PORT": "3306"})
	if err != nil {
		t.Fatal(err)
	}
	want := "# comment\nAPP_NAME=Foo\nDB_HOST=127.0.0.1\n\nSECRET=abc # keep\nDB_PORT=3306\n"
	if out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestApplyKeepsCRLFAndIsIdempotent(t *testing.T) {
	in := "A=1\r\nDB_HOST=x\r\n"
	v := map[string]string{"DB_HOST": "h"}
	once, _ := Apply(in, v)
	twice, _ := Apply(once, v)
	if once != "A=1\r\nDB_HOST=h\r\n" || once != twice {
		t.Fatalf("got %q / %q", once, twice)
	}
}

func TestApplyRejectsUnmanagedKey(t *testing.T) {
	if _, err := Apply("", map[string]string{"APP_KEY": "x"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestUpdateFileBackupAndExampleCopy(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, ".env.example"), []byte("DB_HOST=a\n"), 0o600)
	if err := UpdateFile(d, map[string]string{"DB_HOST": "b"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(d, ".env")); string(b) != "DB_HOST=b\n" {
		t.Fatalf("got %q", b)
	}
	if err := UpdateFile(d, map[string]string{"DB_HOST": "c"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(d, ".env.hyper-backup")); string(b) != "DB_HOST=b\n" {
		t.Fatalf("backup %q", b)
	}
}
