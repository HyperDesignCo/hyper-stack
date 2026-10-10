package direct

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/hyperdesign/hyper-stack/internal/nginx"
	"github.com/hyperdesign/hyper-stack/internal/process"
	rt "github.com/hyperdesign/hyper-stack/internal/runtime"
	"github.com/hyperdesign/hyper-stack/internal/secrets"
	"github.com/hyperdesign/hyper-stack/templates"
)

// PHPService names the worker pool of a PHP version, e.g. PHPService("8.3").
func PHPService(version string) rt.Service { return rt.Service("php-" + version) }

func phpVersionOf(svc rt.Service) (string, bool) {
	s := string(svc)
	if strings.HasPrefix(s, "php-") {
		return strings.TrimPrefix(s, "php-"), true
	}
	return "", false
}

func (r *Runtime) nginxSpec() process.Spec {
	return process.Spec{
		Name: "nginx", Bin: filepath.Join(r.nginxDir(), "nginx.exe"),
		Args:   []string{"-p", r.nginxRun() + string(filepath.Separator), "-c", filepath.Join(r.nginxRun(), "nginx.conf")},
		Dir:    r.nginxRun(),
		RunDir: r.P.Run, LogFile: filepath.Join(r.P.Logs, "nginx.out.log"),
	}
}

func (r *Runtime) mysqlSpec() process.Spec {
	return process.Spec{
		Name: "mysql", Bin: filepath.Join(r.mysqlDir(), "bin", "mysqld.exe"),
		Args:   []string{"--defaults-file=" + r.myCnf(), "--console"},
		RunDir: r.P.Run, LogFile: filepath.Join(r.P.Logs, "mysql.out.log"),
	}
}

func (r *Runtime) phpSpec(version string, i int) (process.Spec, error) {
	env := []string{"PHP_FCGI_MAX_REQUESTS=500", "PHPRC=" + r.phpDir(version)}
	// phpMyAdmin reads these (a stored password of an older install never touches a file).
	if pw := secrets.RootPassword(); pw != "" {
		env = append(env, "HYPER_MYSQL_PASSWORD="+pw)
	}
	tmp := filepath.Join(r.P.Run, "pma-tmp")
	_ = os.MkdirAll(tmp, 0o755)
	env = append(env, fmt.Sprintf("HYPER_MYSQL_PORT=%d", r.S.MySQLPort), "HYPER_PMA_TMP="+tmp)
	return process.Spec{
		Name: fmt.Sprintf("php-%s-%d", version, i), Bin: r.phpCgi(version),
		Args:   []string{"-b", fmt.Sprintf("127.0.0.1:%d", r.FPMPort(version, i)), "-c", filepath.Join(r.phpDir(version), "php.ini")},
		Env:    env,
		RunDir: r.P.Run, LogFile: filepath.Join(r.P.Logs, fmt.Sprintf("php-%s.out.log", version)),
	}, nil
}

// StartService implements runtime.Runtime.
func (r *Runtime) StartService(ctx context.Context, svc rt.Service) error {
	if err := r.requireWindows(); err != nil {
		return err
	}
	if v, ok := phpVersionOf(svc); ok {
		for i := 0; i < FPMWorkers; i++ {
			s, err := r.phpSpec(v, i)
			if err != nil {
				return err
			}
			if err := process.Start(s); err != nil {
				return err
			}
		}
		return waitPort(ctx, r.FPMPort(v, 0), 15*time.Second, "PHP "+v)
	}
	switch svc {
	case rt.Nginx:
		if err := r.writeNginxConf(); err != nil {
			return err
		}
		if process.Running(r.nginxSpec()) {
			return r.ReloadWeb(ctx)
		}
		if err := process.Start(r.nginxSpec()); err != nil {
			return err
		}
		return waitPort(ctx, r.S.HTTPSPort, 15*time.Second, "nginx")
	case rt.MySQL:
		if err := r.writeMyCnf(); err != nil {
			return err
		}
		if err := process.Start(r.mysqlSpec()); err != nil {
			return err
		}
		return waitPort(ctx, r.S.MySQLPort, 60*time.Second, "MySQL")
	}
	return fmt.Errorf("service %q is not supported yet", svc)
}

// StopService implements runtime.Runtime.
func (r *Runtime) StopService(ctx context.Context, svc rt.Service) error {
	if v, ok := phpVersionOf(svc); ok {
		for i := 0; i < FPMWorkers; i++ {
			s, _ := r.phpSpec(v, i)
			if err := process.Stop(s); err != nil {
				return err
			}
		}
		return nil
	}
	switch svc {
	case rt.Nginx:
		return process.Stop(r.nginxSpec())
	case rt.MySQL:
		return process.Stop(r.mysqlSpec())
	}
	return fmt.Errorf("service %q is not supported yet", svc)
}

// ServiceStatus implements runtime.Runtime.
func (r *Runtime) ServiceStatus(ctx context.Context, svc rt.Service) (rt.Status, error) {
	if v, ok := phpVersionOf(svc); ok {
		s, _ := r.phpSpec(v, 0)
		return rt.Status{Running: process.Running(s), PID: process.PID(s), Port: r.FPMPort(v, 0)}, nil
	}
	switch svc {
	case rt.Nginx:
		s := r.nginxSpec()
		return rt.Status{Running: process.Running(s), PID: process.PID(s), Port: r.S.HTTPSPort}, nil
	case rt.MySQL:
		s := r.mysqlSpec()
		return rt.Status{Running: process.Running(s), PID: process.PID(s), Port: r.S.MySQLPort}, nil
	}
	return rt.Status{}, fmt.Errorf("service %q is not supported yet", svc)
}

// RegisterSite writes the nginx vhost for a site and reloads the web server.
func (r *Runtime) RegisterSite(ctx context.Context, site rt.Site) error {
	if err := r.requireWindows(); err != nil {
		return err
	}
	conf, err := nginx.Render(nginx.Site{
		Domain: site.Domain, Root: filepath.ToSlash(site.Root), WebRoot: filepath.ToSlash(site.WebRoot),
		FPMAddr: UpstreamName(site.PHP), CertFile: filepath.ToSlash(site.CertPEM), KeyFile: filepath.ToSlash(site.KeyPEM),
		LogDir: filepath.ToSlash(r.P.Logs), HTTPPort: r.S.HTTPPort, HTTPSPort: r.S.HTTPSPort,
	})
	if err != nil {
		return err
	}
	dir := filepath.Join(r.nginxRun(), "sites")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, site.Name+".conf")
	old, readErr := os.ReadFile(path)
	if readErr == nil && string(old) == conf {
		return nil
	}
	if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
		return err
	}
	// All sites share one nginx. Check before this site goes live, so one bad
	// project can only fail itself. A broken file left by something else (an
	// older Hyper Stack, a hand edit) is set aside instead of blocking everyone.
	for attempt := 0; ; attempt++ {
		out, err := r.testNginx()
		if err == nil {
			return nil
		}
		if bad := offendingConf(out, dir); bad != "" && !strings.EqualFold(bad, path) && attempt < 8 {
			if os.Rename(bad, bad+".rejected") == nil {
				r.Log("set aside a broken web config: %s (it is rebuilt the next time that project starts)", filepath.Base(bad))
				continue
			}
		}
		if readErr == nil {
			_ = os.WriteFile(path, old, 0o644) //nolint:gosec // site config inside our own data directory; the name is a validated slug
		} else {
			_ = os.Remove(path)
		}
		return fmt.Errorf("the web server rejected the configuration for %q, so it was not activated (the other projects are unaffected): %s", site.Name, strings.TrimSpace(out))
	}
}

var confInError = regexp.MustCompile(`in ((?:[A-Za-z]:)?[^\s"]+\.conf):\d+`)

// offendingConf returns the site config file that "nginx -t" complained about,
// if it is one of ours.
func offendingConf(out, sitesDir string) string {
	m := confInError.FindStringSubmatch(out)
	if m == nil {
		return ""
	}
	f := filepath.FromSlash(m[1])
	rel, err := filepath.Rel(sitesDir, f)
	if err != nil || strings.HasPrefix(rel, "..") || strings.ContainsRune(rel, filepath.Separator) {
		return ""
	}
	return f
}

// testNginx runs "nginx -t" against the current configuration.
func (r *Runtime) testNginx() (string, error) {
	if err := r.writeNginxConf(); err != nil {
		return "", err
	}
	s := r.nginxSpec()
	return execCombined(s.Bin, s.Dir, append([]string{"-t"}, s.Args...)...)
}

// UnregisterSite removes a site's vhost.
func (r *Runtime) UnregisterSite(ctx context.Context, name string) error {
	err := os.Remove(filepath.Join(r.nginxRun(), "sites", name+".conf"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ReloadWeb regenerates nginx.conf and reloads (or starts) nginx.
func (r *Runtime) ReloadWeb(ctx context.Context) error {
	if err := r.writeNginxConf(); err != nil {
		return err
	}
	s := r.nginxSpec()
	if !process.Running(s) {
		return r.StartService(ctx, rt.Nginx)
	}
	args := append([]string{"-s", "reload"}, s.Args...)
	out, err := execCombined(s.Bin, s.Dir, args...)
	if err != nil {
		return fmt.Errorf("nginx reload failed: %w\n%s\nCheck %s", err, out, filepath.Join(r.P.Logs, "nginx-error.log"))
	}
	return nil
}

type upstream struct {
	Name    string
	Servers []string
}

func (r *Runtime) writeNginxConf() error {
	run := r.nginxRun()
	for _, d := range []string{run, filepath.Join(run, "sites"), filepath.Join(run, "temp"), filepath.Join(run, "logs"), r.P.Logs} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	for _, f := range []string{"mime.types", "fastcgi_params"} {
		b, err := os.ReadFile(filepath.Join(r.nginxDir(), "conf", f))
		if err != nil {
			return fmt.Errorf("nginx install is incomplete (%s missing): run hyper install", f)
		}
		if err := os.WriteFile(filepath.Join(run, f), b, 0o644); err != nil { //nolint:gosec // site config file inside our own data directory
			return err
		}
	}
	var ups []upstream
	entries, _ := os.ReadDir(filepath.Join(r.P.Bin, "php"))
	for _, e := range entries {
		if !e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
			continue
		}
		u := upstream{Name: UpstreamName(e.Name())}
		for i := 0; i < FPMWorkers; i++ {
			u.Servers = append(u.Servers, fmt.Sprintf("127.0.0.1:%d", r.FPMPort(e.Name(), i)))
		}
		ups = append(ups, u)
	}
	conf, err := templates.Render("nginx.conf.tmpl", map[string]any{
		"LogDir": filepath.ToSlash(r.P.Logs), "PidFile": filepath.ToSlash(filepath.Join(run, "logs", "nginx.pid")),
		"ConfDir": filepath.ToSlash(run), "SitesDir": filepath.ToSlash(filepath.Join(run, "sites")), "Upstreams": ups,
	})
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(run, "nginx.conf"), []byte(conf), 0o644)
}

func waitPort(ctx context.Context, port int, timeout time.Duration, what string) error {
	deadline := time.Now().Add(timeout)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
			_ = c.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return fmt.Errorf("%s did not start listening on %s within %s. See the logs in the Hyper Stack logs folder", what, addr, timeout)
}

// SiteRegistered reports whether a vhost file exists for the site.
func (r *Runtime) SiteRegistered(name string) bool {
	_, err := os.Stat(filepath.Join(r.nginxRun(), "sites", name+".conf"))
	return err == nil
}
