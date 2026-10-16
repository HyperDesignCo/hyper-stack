package direct

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hyperdesign/hyper-stack/internal/config"
	"github.com/hyperdesign/hyper-stack/internal/download"
	rt "github.com/hyperdesign/hyper-stack/internal/runtime"
)

// asMac makes the runtime behave as on a Mac for one test, on any machine.
func asMac(t *testing.T) *Runtime {
	t.Helper()
	oldOS, oldArch := goos, goarch
	goos, goarch = "darwin", "arm64"
	t.Cleanup(func() { goos, goarch = oldOS, oldArch })
	p := config.PathsFor(filepath.Join(t.TempDir(), "stack"))
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	s := config.DefaultSettings()
	s.HTTPPort, s.HTTPSPort, s.MySQLPort = 8080, 8443, 3307
	return New(p, s, nil)
}

func containsEnv(env []string, want string) bool {
	for _, e := range env {
		if e == want {
			return true
		}
	}
	return false
}

func TestMacBinaryNamesHaveNoExe(t *testing.T) {
	r := asMac(t)
	if filepath.Base(r.PHPExe("8.3")) != "php" || filepath.Base(r.mysqlBin("mysqld")) != "mysqld" {
		t.Errorf("macOS programs have no .exe: %s %s", r.PHPExe("8.3"), r.mysqlBin("mysqld"))
	}
	goos = "windows"
	if filepath.Base(r.PHPExe("8.3")) != "php.exe" || filepath.Base(r.mysqlBin("mysqld")) != "mysqld.exe" {
		t.Errorf("Windows programs keep .exe: %s %s", r.PHPExe("8.3"), r.mysqlBin("mysqld"))
	}
}

func TestMacPicksCaddyAndOnePHPPoolPerVersion(t *testing.T) {
	r := asMac(t)
	if r.WebName() != "caddy" || r.webSpec().Name != "caddy" || r.workerCount() != 1 {
		t.Fatalf("web=%s spec=%s workers=%d", r.WebName(), r.webSpec().Name, r.workerCount())
	}
	spec, _ := r.phpSpec("8.3", 0)
	if spec.Name != "php-8.3-0" || filepath.Base(spec.Bin) != "php-fpm" {
		t.Errorf("php-fpm spec wrong: %+v", spec)
	}
	joined := strings.Join(spec.Args, " ")
	if !strings.Contains(joined, "-y "+r.fpmConf("8.3")) || !strings.Contains(joined, filepath.Join(r.phpDir("8.3"), "php.ini")) {
		t.Errorf("php-fpm must be given its pool file and php.ini: %v", spec.Args)
	}
	if !containsEnv(spec.Env, "PHPRC="+r.phpDir("8.3")) {
		t.Errorf("a static PHP finds php.ini through PHPRC: %v", spec.Env)
	}
	goos = "windows"
	if r.WebName() != "nginx" || r.workerCount() != FPMWorkers {
		t.Error("Windows keeps nginx and several php-cgi workers")
	}
}

func TestMacWritesTheFPMPoolAndCaddyfile(t *testing.T) {
	r := asMac(t)
	if err := r.writeFPMConf("8.3"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(r.fpmConf("8.3"))
	for _, want := range []string{"daemonize = no", "listen = 127.0.0.1:9030", "pm = static", "pm.max_children = 4"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("php-fpm pool missing %q:\n%s", want, b)
		}
	}
	if err := r.writeCaddyConf(); err != nil {
		t.Fatal(err)
	}
	c, _ := os.ReadFile(r.caddyfile())
	for _, want := range []string{"http_port 8080", "https_port 8443", "admin " + caddyAdmin, "auto_https off", "/*.caddy"} {
		if !strings.Contains(string(c), want) {
			t.Errorf("Caddyfile missing %q:\n%s", want, c)
		}
	}
}

func TestMacSiteGetsACaddyFileWithItsOwnPHPPort(t *testing.T) {
	r := asMac(t)
	conf, err := r.renderSite(rt.Site{Name: "blog", Domain: "blog.test", Root: "/w/blog", WebRoot: "/w/blog/public", PHP: "8.1", CertPEM: "/c/a.pem", KeyPEM: "/c/a.key"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"https://blog.test:8443", `root * "/w/blog/public"`, "php_fastcgi 127.0.0.1:9010", "http://blog.test:8080"} {
		if !strings.Contains(conf, want) {
			t.Errorf("missing %q in:\n%s", want, conf)
		}
	}
	if r.webSiteExt() != ".caddy" {
		t.Errorf("extension: %s", r.webSiteExt())
	}
	goos = "windows"
	if r.webSiteExt() != ".conf" {
		t.Errorf("extension: %s", r.webSiteExt())
	}
}

func TestMacUnregisterRemovesTheSiteFile(t *testing.T) {
	r := asMac(t)
	if err := os.MkdirAll(r.webSitesDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.webSitesDir(), "blog.caddy"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !r.SiteRegistered("blog") {
		t.Fatal("the site file must be recognised")
	}
	if err := r.UnregisterSite(context.Background(), "blog"); err != nil || r.SiteRegistered("blog") {
		t.Fatalf("unregister: %v registered=%v", err, r.SiteRegistered("blog"))
	}
}

func TestMacMySQLUsesASocketInTmp(t *testing.T) {
	r := asMac(t)
	if got := r.mysqlSocket(); got != "/tmp/hyper-mysql-3307.sock" {
		t.Errorf("socket: %s", got)
	}
	goos = "windows"
	if r.mysqlSocket() != "" {
		t.Error("Windows has no socket line")
	}
}

func TestOffendingConfUnderstandsCaddyMessages(t *testing.T) {
	sites := filepath.Join(string(filepath.Separator), "hs", "run", "caddy", "sites")
	msg := "Error: adapting config using caddyfile: " + filepath.Join(sites, "bad.caddy") + ":12 - Error during parsing: unknown directive"
	if got := offendingConf(msg, sites); got != filepath.Join(sites, "bad.caddy") {
		t.Errorf("got %q", got)
	}
}

func TestMacArchitectures(t *testing.T) {
	a, err := macArchFor("arm64")
	if err != nil || a.caddy != "arm64" || a.mysql != "arm64" || a.node != "arm64" || a.static != "aarch64" {
		t.Errorf("arm64: %+v %v", a, err)
	}
	a, err = macArchFor("amd64")
	if err != nil || a.caddy != "amd64" || a.mysql != "x86_64" || a.node != "x64" || a.static != "x86_64" {
		t.Errorf("amd64: %+v %v", a, err)
	}
	if _, err := macArchFor("386"); err == nil {
		t.Error("an unsupported processor must be refused")
	}
}

// Every download must be pinned, so nothing installs unverified.
func TestMacDownloadsAreAllPinned(t *testing.T) {
	for _, goarch := range []string{"arm64", "amd64"} {
		a, _ := macArchFor(goarch)
		for _, v := range []string{"8.0", "8.1", "8.2", "8.3", "8.4"} {
			cli, fpm, ok := StaticPHP(v, a)
			if !ok {
				t.Fatalf("PHP %s (%s) must be available on macOS", v, goarch)
			}
			for _, art := range []Artifact{cli, fpm} {
				if len(art.Checksum) != 64 || art.Algo != download.SHA256 || !strings.HasPrefix(art.URL, "https://") {
					t.Errorf("PHP %s %s: not pinned properly: %+v", v, goarch, art)
				}
			}
			if cli.URL == fpm.URL || !strings.Contains(cli.URL, "-cli-macos-"+a.static) || !strings.Contains(fpm.URL, "-fpm-macos-"+a.static) {
				t.Errorf("PHP %s (%s): wrong files: %s / %s", v, goarch, cli.URL, fpm.URL)
			}
		}
		m := MySQLMac(a)
		if len(m.Checksum) != 32 || m.Algo != download.MD5 || !strings.Contains(m.URL, "macos15-"+a.mysql) || !m.StripTop {
			t.Errorf("MySQL (%s): %+v", goarch, m)
		}
	}
	if _, _, ok := StaticPHP("7.4", macArch{static: "aarch64"}); ok {
		t.Error("there is no PHP 7.4 for macOS: it must say so rather than pretend")
	}
	if OldestMacPHP() != "8.0" {
		t.Errorf("oldest: %s", OldestMacPHP())
	}
}

func TestNodeMacPicksTheRightArchive(t *testing.T) {
	sums := "aaa111  node-v20.20.2-darwin-arm64.tar.gz\nbbb222  node-v20.20.2-darwin-x64.tar.gz\nccc333  node-v20.20.2-win-x64.zip\n"
	a, _ := macArchFor("amd64")
	art, err := nodeMacFrom(sums, "https://nodejs.org/dist/latest-v20.x/", "20", a)
	if err != nil || art.Checksum != "bbb222" || !strings.HasSuffix(art.URL, "node-v20.20.2-darwin-x64.tar.gz") || !art.StripTop {
		t.Errorf("got %+v %v", art, err)
	}
	if _, err := nodeMacFrom("zzz  other.zip\n", "x/", "20", a); err == nil {
		t.Error("a missing archive must be an error")
	}
}

func TestMacRefusesAPHPItDoesNotHave(t *testing.T) {
	r := asMac(t)
	_, err := r.EnsurePHP(context.Background(), "7.4", nil)
	if err == nil || !strings.Contains(err.Error(), "not available on macOS") || !strings.Contains(err.Error(), "8.0") {
		t.Errorf("PHP 7.4 on a Mac must be refused with the oldest available: %v", err)
	}
}

func TestAlgoFollowsDigestLength(t *testing.T) {
	if algoByLength(strings.Repeat("a", 128)) != download.SHA512 || algoByLength(strings.Repeat("a", 64)) != download.SHA256 {
		t.Error("128 hex digits is SHA-512, 64 is SHA-256")
	}
}

// skip-name-resolve breaks root@localhost for TCP logins from 127.0.0.1
// (ERROR 1130, found on a real machine), so the config must never carry it.
func TestMyCnfKeepsNameResolution(t *testing.T) {
	r := asMac(t)
	if err := r.writeMyCnf(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(r.myCnf())
	if strings.Contains(string(b), "skip-name-resolve") {
		t.Errorf("my.cnf must not use skip-name-resolve:\n%s", b)
	}
}
