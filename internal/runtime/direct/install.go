package direct

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hyperdesign/hyper-stack/internal/config"
	"github.com/hyperdesign/hyper-stack/internal/download"
	rt "github.com/hyperdesign/hyper-stack/internal/runtime"
	"github.com/hyperdesign/hyper-stack/templates"
)

// BaseExtensions are enabled for every PHP version (what Laravel needs).
var BaseExtensions = []string{"curl", "fileinfo", "gd", "intl", "mbstring", "exif", "mysqli", "openssl", "pdo_mysql", "pdo_sqlite", "sodium", "sqlite3", "zip"}

// Runtime manages official binaries under the Hyper Stack data directory.
type Runtime struct {
	P   config.Paths
	S   config.Settings
	Log func(format string, args ...any)
}

// New creates a Runtime. log may be nil.
func New(p config.Paths, s config.Settings, log func(string, ...any)) *Runtime {
	if log == nil {
		log = func(string, ...any) {}
	}
	return &Runtime{P: p, S: s, Log: log}
}

var _ rt.Runtime = (*Runtime)(nil)

// Name implements runtime.Runtime.
func (r *Runtime) Name() string { return "direct" }

// Paths implements runtime.Runtime.
func (r *Runtime) Paths() rt.RuntimePaths {
	return rt.RuntimePaths{Data: r.P.Root, Logs: r.P.Logs, Config: r.P.Run}
}

func (r *Runtime) requireWindows() error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("the macOS runtime (static PHP + Caddy) is not implemented yet; only Windows is supported in this build")
	}
	return nil
}

// Directory helpers.
func (r *Runtime) nginxDir() string       { return filepath.Join(r.P.Bin, "nginx") }
func (r *Runtime) mysqlDir() string       { return filepath.Join(r.P.Bin, "mysql") }
func (r *Runtime) phpDir(v string) string { return filepath.Join(r.P.Bin, "php", v) }
func (r *Runtime) nodeDir() string        { return filepath.Join(r.P.Bin, "node") }
func (r *Runtime) pmaDir() string         { return filepath.Join(r.P.Bin, "phpmyadmin") }
func (r *Runtime) composerPhar() string   { return filepath.Join(r.P.Bin, "composer", "composer.phar") }
func (r *Runtime) caBundle() string       { return filepath.Join(r.P.Bin, "cacert.pem") }
func (r *Runtime) mysqlData() string      { return filepath.Join(r.P.Root, "data", "mysql") }
func (r *Runtime) nginxRun() string       { return filepath.Join(r.P.Run, "nginx") }

// PHPExe and PHPCgi return php binaries for a version.
func (r *Runtime) PHPExe(v string) string { return filepath.Join(r.phpDir(v), "php.exe") }
func (r *Runtime) phpCgi(v string) string { return filepath.Join(r.phpDir(v), "php-cgi.exe") }

// NodeDir returns the Node install directory (contains node.exe and npm.cmd).
func (r *Runtime) NodeDir() string { return r.nodeDir() }

// ComposerPhar returns the composer.phar path.
func (r *Runtime) ComposerPhar() string { return r.composerPhar() }

// PMADir returns the phpMyAdmin directory.
func (r *Runtime) PMADir() string { return r.pmaDir() }

// FPMPort returns the port of worker i of a PHP version ("8.3" -> base+30+i).
func (r *Runtime) FPMPort(version string, i int) int {
	minor := 0
	if parts := strings.Split(version, "."); len(parts) == 2 {
		fmt.Sscanf(parts[1], "%d", &minor)
	}
	return r.S.FPMBasePort + minor*10 + i
}

// FPMWorkers is the number of php-cgi processes per version (php-cgi on
// Windows handles one request at a time, so several run behind an upstream).
const FPMWorkers = 4

// UpstreamName is the nginx upstream for a PHP version.
func UpstreamName(version string) string { return "php" + strings.ReplaceAll(version, ".", "") }

// EnsureInstalled downloads and configures everything in spec.
func (r *Runtime) EnsureInstalled(ctx context.Context, spec rt.StackSpec) error {
	if err := r.requireWindows(); err != nil {
		return err
	}
	if err := r.P.Ensure(); err != nil {
		return err
	}
	if err := r.installArtifact(Nginx(), r.nginxDir()); err != nil {
		return err
	}
	if err := r.installArtifact(MySQLWindows(), r.mysqlDir()); err != nil {
		return err
	}
	if !fileExists(r.caBundle()) {
		ca, err := CABundle(ctx)
		if err != nil {
			return err
		}
		if err := r.installFile(ca, r.caBundle()); err != nil {
			return err
		}
	}
	if !fileExists(r.composerPhar()) {
		comp, err := Composer(ctx)
		if err != nil {
			return err
		}
		if err := r.installFile(comp, r.composerPhar()); err != nil {
			return err
		}
	}
	if !r.isInstalled(r.nodeDir()) {
		node, err := NodeWindows(ctx, spec.NodeVersion)
		if err != nil {
			return err
		}
		if err := r.installArtifact(node, r.nodeDir()); err != nil {
			return err
		}
	}
	if !r.isInstalled(r.pmaDir()) {
		pma, err := PHPMyAdmin(ctx)
		if err != nil {
			return err
		}
		if err := r.installArtifact(pma, r.pmaDir()); err != nil {
			return err
		}
	}
	for _, v := range spec.PHPVersions {
		if _, err := r.EnsurePHP(ctx, v, nil); err != nil {
			return err
		}
	}
	return nil
}

// EnsurePHP installs a PHP version and (re)writes its php.ini with the base
// extensions plus extra ones. Extra extensions must be bundled with PHP.
func (r *Runtime) EnsurePHP(ctx context.Context, version string, extra []string) (rt.PHPInstall, error) {
	if err := r.requireWindows(); err != nil {
		return rt.PHPInstall{}, err
	}
	if !r.isInstalled(r.phpDir(version)) {
		art, err := PHPWindows(ctx, version)
		if err != nil {
			return rt.PHPInstall{}, err
		}
		if err := r.installArtifact(art, r.phpDir(version)); err != nil {
			return rt.PHPInstall{}, err
		}
	}
	if err := r.writePHPIni(version, extra); err != nil {
		return rt.PHPInstall{}, err
	}
	return rt.PHPInstall{Version: version, Binary: r.PHPExe(version), FPMAddr: UpstreamName(version)}, nil
}

func (r *Runtime) writePHPIni(version string, extra []string) error {
	dir := r.phpDir(version)
	ext := filepath.Join(dir, "ext")
	builtin, err := r.builtinExtensions(version)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var enable []string
	for _, e := range append(append([]string{}, BaseExtensions...), extra...) {
		if seen[e] {
			continue
		}
		seen[e] = true
		if _, err := os.Stat(filepath.Join(ext, "php_"+e+".dll")); err == nil {
			enable = append(enable, e)
			continue
		}
		if builtin[strings.ToLower(e)] {
			continue
		}
		return fmt.Errorf("PHP %s extension %q is not bundled with the official Windows build. "+
			"Remove it from \"extensions\" in hyper.json or ask the platform team to add support", version, e)
	}
	ini, err := templates.Render("php.ini.tmpl", map[string]any{
		"LogFile":    filepath.ToSlash(filepath.Join(r.P.Logs, "php-"+version+".log")),
		"ExtDir":     ext,
		"CABundle":   r.caBundle(),
		"Extensions": enable,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "php.ini"), []byte(ini), 0o644)
}

// builtinExtensions lists extensions compiled into php.exe.
func (r *Runtime) builtinExtensions(version string) (map[string]bool, error) {
	out, err := exec.Command(r.PHPExe(version), "-n", "-m").Output()
	if err != nil {
		return nil, fmt.Errorf("cannot run %s: %w", r.PHPExe(version), err)
	}
	m := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(strings.ToLower(l)); l != "" && !strings.HasPrefix(l, "[") {
			m[l] = true
		}
	}
	return m, nil
}

const installedMarker = ".hyper-installed"

// installArtifact downloads, verifies and unpacks into dest atomically.
func (r *Runtime) installArtifact(a Artifact, dest string) error {
	if b, _ := os.ReadFile(filepath.Join(dest, installedMarker)); string(b) == a.Checksum {
		return nil
	}
	cache := filepath.Join(r.P.Root, "cache", filepath.Base(a.URL))
	if err := r.fetch(a, cache); err != nil {
		return err
	}
	r.Log("Unpacking %s", a.Name)
	tmp := dest + ".tmp"
	os.RemoveAll(tmp)
	if err := download.Unpack(cache, tmp, a.StripTop); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, installedMarker), []byte(a.Checksum), 0o644); err != nil {
		return err
	}
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("cannot replace %s (is a service using it?): %w", dest, err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dest)
}

// installFile downloads a single (non-archive) file.
func (r *Runtime) installFile(a Artifact, dest string) error {
	return r.fetch(a, dest)
}

func (r *Runtime) fetch(a Artifact, dest string) error {
	r.Log("Downloading %s", a.Name)
	last := -1
	return download.File(context.Background(), a.URL, dest, a.Algo, a.Checksum, func(done, total int64) {
		if total <= 0 {
			return
		}
		if pct := int(done * 100 / total); pct/25 != last/25 {
			last = pct
			r.Log("  %s: %d%%", a.Name, pct)
		}
	})
}

// isInstalled reports whether a component finished unpacking (no network needed).
func (r *Runtime) isInstalled(dest string) bool {
	return fileExists(filepath.Join(dest, installedMarker))
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
