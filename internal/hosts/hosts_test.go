package hosts

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const base = "127.0.0.1 localhost\n10.0.0.5 corp.example\n"

func TestRenderAddsBlockAndKeepsOutside(t *testing.T) {
	out := Render(base, []string{"b.test", "a.test", "a.test"})
	want := base + "# >>> hyper-stack >>>\n127.0.0.1 a.test\n127.0.0.1 b.test\n# <<< hyper-stack <<<\n"
	if out != want {
		t.Fatalf("got %q", out)
	}
}

func TestRenderReplacesAndIsIdempotent(t *testing.T) {
	once := Render(base, []string{"a.test"})
	if Render(once, []string{"a.test"}) != once {
		t.Fatal("not idempotent")
	}
	if got := Domains(Render(once, []string{"c.test"})); !reflect.DeepEqual(got, []string{"c.test"}) {
		t.Fatalf("got %v", got)
	}
}

func TestRenderEmptyRemovesBlock(t *testing.T) {
	if got := Render(Render(base, []string{"a.test"}), nil); got != base {
		t.Fatalf("got %q", got)
	}
}

func TestUpdateUsesTempFileAndBacksUp(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hosts")
	os.WriteFile(p, []byte(base), 0o644)
	bk := filepath.Join(t.TempDir(), "hosts.bak")
	changed, err := Update(p, bk, []string{"a.test"})
	if err != nil || !changed {
		t.Fatalf("changed=%v err=%v", changed, err)
	}
	if b, _ := os.ReadFile(bk); string(b) != base {
		t.Fatalf("backup %q", b)
	}
	if changed, _ := Update(p, bk, []string{"a.test"}); changed {
		t.Fatal("second update should be a no-op")
	}
}
