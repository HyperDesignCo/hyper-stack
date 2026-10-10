package project

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSlugAndDBName(t *testing.T) {
	cases := map[string]string{"My Shop_2": "my-shop-2", "bazooka": "bazooka", "  --Hello World--": "hello-world", "مشروع": "", "a.b.c": "a-b-c"}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
	if DBName("my-shop-2") != "my_shop_2" {
		t.Error("DBName must use underscores")
	}
}

func TestLowerBound(t *testing.T) {
	cases := []struct {
		in    string
		want  ver
		known bool
	}{
		{"^8.3", ver{8, 3}, true},
		{">=8.4.1", ver{8, 4}, true},
		{"~8.1.0", ver{8, 1}, true},
		{"8.2.*", ver{8, 2}, true},
		{"^8.1 || ^8.2", ver{8, 1}, true},
		{">=8.0 <8.4", ver{8, 0}, true},
		{"<8.4", ver{}, false},
		{"*", ver{}, false},
		{"", ver{}, false},
		{"^7.4|^8.0", ver{7, 4}, true},
	}
	for _, c := range cases {
		got, ok := lowerBound(c.in)
		if ok != c.known || (ok && got != c.want) {
			t.Errorf("lowerBound(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.known)
		}
	}
}

func TestDetectPHP(t *testing.T) {
	t.Run("no composer files", func(t *testing.T) {
		if got := DetectPHP(t.TempDir()); got != DefaultPHP {
			t.Fatalf("got %s", got)
		}
	})
	t.Run("composer.json only", func(t *testing.T) {
		d := t.TempDir()
		write(t, d, "composer.json", `{"require":{"php":"^8.2"}}`)
		if got := DetectPHP(d); got != "8.2" {
			t.Fatalf("got %s", got)
		}
	})
	t.Run("the lock needs more than composer.json says (Bazooka)", func(t *testing.T) {
		d := t.TempDir()
		write(t, d, "composer.json", `{"require":{"php":"^8.3"}}`)
		write(t, d, "composer.lock", `{"packages":[{"name":"symfony/console","require":{"php":">=8.4.1"}},{"name":"x/y","require":{"php":"^8.2"}}],"packages-dev":[]}`)
		if got := DetectPHP(d); got != "8.4" {
			t.Fatalf("got %s, want 8.4", got)
		}
	})
	t.Run("an old project keeps an old PHP so it can run as it is", func(t *testing.T) {
		d := t.TempDir()
		write(t, d, "composer.json", `{"require":{"php":"^7.1.3"}}`) // Laravel 5.8
		if got := DetectPHP(d); got != "7.4" {
			t.Fatalf("Laravel 5.8 needs PHP 7.x: got %s", got)
		}
	})
	t.Run("Laravel 9 picks 8.0, Laravel 10 picks 8.1", func(t *testing.T) {
		for want, constraint := range map[string]string{"8.0": "^8.0.2", "8.1": "^8.1", "8.2": "^8.2"} {
			d := t.TempDir()
			write(t, d, "composer.json", `{"require":{"php":"`+constraint+`"}}`)
			if got := DetectPHP(d); got != want {
				t.Errorf("%s: got %s, want %s", constraint, got, want)
			}
		}
	})
	t.Run("older than anything we offer falls forward to 7.4", func(t *testing.T) {
		d := t.TempDir()
		write(t, d, "composer.json", `{"require":{"php":"^5.6"}}`)
		if got := DetectPHP(d); got != "7.4" {
			t.Fatalf("got %s", got)
		}
	})
	t.Run("needs a PHP we do not offer", func(t *testing.T) {
		d := t.TempDir()
		write(t, d, "composer.json", `{"require":{"php":"^9.0"}}`)
		if got := DetectPHP(d); got != "8.4" {
			t.Fatalf("got %s", got)
		}
	})
	t.Run("broken json is ignored", func(t *testing.T) {
		d := t.TempDir()
		write(t, d, "composer.json", `{not json`)
		if got := DetectPHP(d); got != DefaultPHP {
			t.Fatalf("got %s", got)
		}
	})
}

func TestWebRoot(t *testing.T) {
	d := t.TempDir()
	if WebRoot(d) != d {
		t.Error("plain folder serves itself")
	}
	write(t, d, "public_html/index.php", "x")
	if WebRoot(d) != filepath.Join(d, "public_html") {
		t.Error("public_html")
	}
	write(t, d, "public/index.php", "x")
	if WebRoot(d) != filepath.Join(d, "public") {
		t.Error("public wins")
	}
}

// A project deployed the cPanel way keeps index.php in its root and leaves
// public/ with only assets. Serving public/ gave "403 Forbidden".
func TestWebRootIgnoresAPublicFolderWithoutAnIndex(t *testing.T) {
	d := t.TempDir()
	write(t, d, "index.php", "<?php require 'vendor/autoload.php';")
	write(t, d, "public/css/app.css", "body{}")
	write(t, d, "public/js/app.js", "x")
	if got := WebRoot(d); got != d {
		t.Fatalf("public/ has no index file, so the project root must be served: got %s", got)
	}
	write(t, d, "public/index.php", "<?php")
	if got := WebRoot(d); got != filepath.Join(d, "public") {
		t.Fatalf("once public/ has an index it is the web root: got %s", got)
	}
}

func TestResolveAutoVsHyperJSON(t *testing.T) {
	base := t.TempDir()
	auto := filepath.Join(base, "My Shop")
	write(t, auto, "index.php", "<?php")
	c, isAuto, err := Resolve(auto)
	if err != nil || !isAuto {
		t.Fatalf("auto: %v %v", isAuto, err)
	}
	if c.Name != "my-shop" || c.Domain != "my-shop.test" || c.Database.Name != "my_shop" || c.PHP != DefaultPHP {
		t.Fatalf("auto config wrong: %+v", c)
	}
	if len(c.Setup) != 0 || len(c.Workers) != 0 {
		t.Fatal("an auto project must never run commands")
	}

	explicit := filepath.Join(base, "bazooka")
	write(t, explicit, "hyper.json", `{"name":"bazooka-prod","php":"8.4","database":{"engine":"mysql","name":"b"},"setup":["php -v"]}`)
	c, isAuto, err = Resolve(explicit)
	if err != nil || isAuto || c.Name != "bazooka-prod" || len(c.Setup) != 1 {
		t.Fatalf("hyper.json must win: %+v auto=%v err=%v", c, isAuto, err)
	}

	if _, _, err := Resolve(filepath.Join(base, "مشروع")); err == nil {
		t.Error("a folder name with no usable characters must give a clear error")
	}
}

func TestFilesWithAByteOrderMarkAreAccepted(t *testing.T) {
	bom := string(utf8BOM) // the real UTF-8 byte order mark: EF BB BF
	d := t.TempDir()
	write(t, d, "composer.json", bom+`{"require":{"php":"^8.2"}}`)
	if got := DetectPHP(d); got != "8.2" {
		t.Errorf("composer.json with BOM: got %s", got)
	}
	write(t, d, "hyper.json", bom+`{"name":"x","php":"8.4","database":{"engine":"mysql","name":"x"}}`)
	c, auto, err := Resolve(d)
	if err != nil || auto || c.PHP != "8.4" {
		t.Errorf("hyper.json with BOM: %+v auto=%v err=%v", c, auto, err)
	}
}
