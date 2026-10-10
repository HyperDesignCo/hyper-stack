package direct

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/hyperdesign/hyper-stack/internal/caddy"
	"github.com/hyperdesign/hyper-stack/internal/process"
	rt "github.com/hyperdesign/hyper-stack/internal/runtime"
	"github.com/hyperdesign/hyper-stack/templates"
)

// goos and goarch name the platform the runtime manages. They are variables so
// the macOS paths can be exercised by tests on any machine.
var (
	goos   = runtime.GOOS
	goarch = runtime.GOARCH
)

func isMac() bool { return goos == "darwin" }

// requireSupported refuses platforms Hyper Stack does not run on.
func (r *Runtime) requireSupported() error {
	if goos != "windows" && goos != "darwin" {
		return fmt.Errorf("this program runs on Windows and macOS, not %s", goos)
	}
	return nil
}

// binName adds ".exe" on Windows.
func binName(base string) string {
	if goos == "windows" {
		return base + ".exe"
	}
	return base
}

func (r *Runtime) mysqlBin(name string) string {
	return filepath.Join(r.mysqlDir(), "bin", binName(name))
}

// caddyAdmin is the address of Caddy's admin endpoint, used to reload it. It is
// away from Caddy's default (2019) so another Caddy on the machine does not clash.
const caddyAdmin = "127.0.0.1:2619"

func (r *Runtime) caddyDir() string          { return filepath.Join(r.P.Bin, "caddy") }
func (r *Runtime) caddyBin() string          { return filepath.Join(r.caddyDir(), "caddy") }
func (r *Runtime) caddyRun() string          { return filepath.Join(r.P.Run, "caddy") }
func (r *Runtime) caddyfile() string         { return filepath.Join(r.caddyRun(), "Caddyfile") }
func (r *Runtime) phpFpmDir(v string) string { return filepath.Join(r.P.Bin, "php-fpm", v) }
func (r *Runtime) phpFpmBin(v string) string { return filepath.Join(r.phpFpmDir(v), "php-fpm") }
func (r *Runtime) fpmConf(v string) string   { return filepath.Join(r.P.Run, "php-fpm-"+v+".conf") }

// webRun is the folder holding the web server's generated files.
func (r *Runtime) webRun() string {
	if isMac() {
		return r.caddyRun()
	}
	return r.nginxRun()
}

// webReadyPort is the port that tells us the web server is up. Caddy only opens
// the HTTPS port once a site exists, so with none it is the admin endpoint.
func (r *Runtime) webReadyPort() int {
	if isMac() {
		if entries, _ := os.ReadDir(r.webSitesDir()); len(entries) == 0 {
			return 2619
		}
	}
	return r.S.HTTPSPort
}

// mysqlSocket keeps MySQL's socket in /tmp: a unix socket path may not exceed
// about 100 characters on macOS, which a deep install folder would break.
func (r *Runtime) mysqlSocket() string {
	if isMac() {
		return fmt.Sprintf("/tmp/hyper-mysql-%d.sock", r.S.MySQLPort)
	}
	return ""
}

// ---------------------------------------------------------------- install

func (r *Runtime) ensureMacTools(ctx context.Context, spec rt.StackSpec) error {
	a, err := macArchFor(goarch)
	if err != nil {
		return err
	}
	if !r.isInstalled(r.caddyDir()) {
		c, err := CaddyMac(ctx, a.caddy)
		if err != nil {
			return err
		}
		if err := r.installArtifact(c, r.caddyDir()); err != nil {
			return err
		}
	}
	if err := r.installArtifact(MySQLMac(a), r.mysqlDir()); err != nil {
		return err
	}
	if !r.isInstalled(r.nodeDir()) {
		n, err := NodeMac(ctx, spec.NodeVersion, a)
		if err != nil {
			return err
		}
		return r.installArtifact(n, r.nodeDir())
	}
	return nil
}

// ensurePHPMac installs a static PHP (command line and FPM) and writes its php.ini.
func (r *Runtime) ensurePHPMac(version string, extra []string) (rt.PHPInstall, error) {
	a, err := macArchFor(goarch)
	if err != nil {
		return rt.PHPInstall{}, err
	}
	if err := r.CheckPHP(version); err != nil {
		return rt.PHPInstall{}, err
	}
	cli, fpm, _ := StaticPHP(version, a)
	if err := r.installArtifact(cli, r.phpDir(version)); err != nil {
		return rt.PHPInstall{}, err
	}
	if err := r.installArtifact(fpm, r.phpFpmDir(version)); err != nil {
		return rt.PHPInstall{}, err
	}
	if err := r.writePHPIni(version, extra); err != nil {
		return rt.PHPInstall{}, err
	}
	return rt.PHPInstall{Version: version, Binary: r.PHPExe(version), FPMAddr: fmt.Sprintf("127.0.0.1:%d", r.FPMPort(version, 0))}, nil
}

// ---------------------------------------------------------------- PHP-FPM

func (r *Runtime) phpFpmSpec(version string) process.Spec {
	return process.Spec{
		Name: fmt.Sprintf("php-%s-0", version), Bin: r.phpFpmBin(version),
		Args:   []string{"-y", r.fpmConf(version), "-c", filepath.Join(r.phpDir(version), "php.ini")},
		Env:    r.phpEnv(version),
		RunDir: r.P.Run, LogFile: filepath.Join(r.P.Logs, fmt.Sprintf("php-%s.out.log", version)),
	}
}

// writeFPMConf writes the FPM pool: one master per PHP version with 4 workers.
func (r *Runtime) writeFPMConf(version string) error {
	conf, err := templates.Render("php-fpm.conf.tmpl", map[string]any{
		"Pid":      filepath.Join(r.P.Run, "php-fpm-"+version+".pid"),
		"ErrorLog": filepath.Join(r.P.Logs, "php-fpm-"+version+".log"),
		"Port":     r.FPMPort(version, 0),
		"Children": FPMWorkers,
	})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(r.P.Run, 0o755); err != nil {
		return err
	}
	return os.WriteFile(r.fpmConf(version), []byte(conf), 0o644) //nolint:gosec // config inside our own data directory
}

// ---------------------------------------------------------------- Caddy

func (r *Runtime) caddySpec() process.Spec {
	return process.Spec{
		Name: "caddy", Bin: r.caddyBin(),
		Args:   []string{"run", "--config", r.caddyfile(), "--adapter", "caddyfile"},
		Dir:    r.caddyRun(),
		RunDir: r.P.Run, LogFile: filepath.Join(r.P.Logs, "caddy.out.log"),
	}
}

func (r *Runtime) renderCaddySite(site rt.Site) (string, error) {
	web := site.WebRoot
	if web == "" {
		web = filepath.Join(site.Root, "public")
	}
	return caddy.Render(caddy.Site{
		Domain: site.Domain, WebRoot: web, FPMAddr: fmt.Sprintf("127.0.0.1:%d", r.FPMPort(site.PHP, 0)),
		CertFile: site.CertPEM, KeyFile: site.KeyPEM, LogDir: r.P.Logs,
		HTTPPort: r.S.HTTPPort, HTTPSPort: r.S.HTTPSPort,
	})
}

func (r *Runtime) writeCaddyConf() error {
	for _, d := range []string{r.caddyRun(), r.webSitesDir(), r.P.Logs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	main := caddy.Main(r.webSitesDir(), r.S.HTTPPort, r.S.HTTPSPort, caddyAdmin)
	return os.WriteFile(r.caddyfile(), []byte(main), 0o644) //nolint:gosec // config inside our own data directory
}

// testCaddy validates the configuration (including that each certificate loads).
func (r *Runtime) testCaddy() (string, error) {
	return execCombined(r.caddyBin(), r.caddyRun(), "validate", "--config", r.caddyfile(), "--adapter", "caddyfile")
}

// reloadCaddy applies the configuration to the running Caddy without a restart.
func (r *Runtime) reloadCaddy() error {
	out, err := execCombined(r.caddyBin(), r.caddyRun(), "reload", "--config", r.caddyfile(), "--adapter", "caddyfile", "--address", caddyAdmin)
	if err != nil {
		return fmt.Errorf("caddy reload failed: %w\n%s\nCheck %s", err, out, filepath.Join(r.P.Logs, "caddy.out.log"))
	}
	// A reload applies asynchronously; give it a moment before callers probe the site.
	time.Sleep(200 * time.Millisecond)
	return nil
}

// CheckPHP says whether this machine can run a PHP version, before anything is
// changed on its behalf.
func (r *Runtime) CheckPHP(version string) error {
	if !isMac() {
		return nil
	}
	a, err := macArchFor(goarch)
	if err != nil {
		return err
	}
	if _, _, ok := StaticPHP(version, a); !ok {
		return fmt.Errorf("PHP %s is not available on macOS (the oldest there is %s). "+
			"Windows offers older versions for upgrading old projects", version, OldestMacPHP())
	}
	return nil
}
