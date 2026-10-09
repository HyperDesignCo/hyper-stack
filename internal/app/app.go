// Package app implements the hyper commands. The cli package only parses flags.
package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/hyperdesign/hyper-stack/internal/certs"
	"github.com/hyperdesign/hyper-stack/internal/config"
	"github.com/hyperdesign/hyper-stack/internal/doctor"
	"github.com/hyperdesign/hyper-stack/internal/elevate"
	"github.com/hyperdesign/hyper-stack/internal/envfile"
	"github.com/hyperdesign/hyper-stack/internal/hosts"
	"github.com/hyperdesign/hyper-stack/internal/process"
	"github.com/hyperdesign/hyper-stack/internal/project"
	rt "github.com/hyperdesign/hyper-stack/internal/runtime"
	"github.com/hyperdesign/hyper-stack/internal/runtime/direct"
	"github.com/hyperdesign/hyper-stack/internal/secrets"
	"github.com/hyperdesign/hyper-stack/internal/selfinstall"
	"github.com/hyperdesign/hyper-stack/templates"
)

const (
	pmaDomain   = "phpmyadmin.test"
	pmaPHP      = "8.3" // phpMyAdmin 5.2 does not support PHP 8.4
	defaultPHP  = "8.3"
	defaultNode = "20"
)

// App holds everything a command needs.
type App struct {
	P       config.Paths
	S       config.Settings
	RT      *direct.Runtime
	Out     io.Writer
	Verbose bool
	logFile *os.File
}

// New builds an App with the default data directory.
func New(out io.Writer, verbose bool) (*App, error) {
	p, err := config.Default()
	if err != nil {
		return nil, err
	}
	return NewAt(p, out, verbose)
}

// NewAt builds an App for a specific layout (used by tests).
func NewAt(p config.Paths, out io.Writer, verbose bool) (*App, error) {
	if err := p.Ensure(); err != nil {
		return nil, err
	}
	s, err := p.LoadSettings()
	if err != nil {
		return nil, err
	}
	a := &App{P: p, S: s, Out: out, Verbose: verbose}
	a.logFile, _ = os.OpenFile(filepath.Join(p.Logs, "hyper.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	a.RT = direct.New(p, s, a.logf)
	return a, nil
}

// Close releases the log file.
func (a *App) Close() {
	if a.logFile != nil {
		a.logFile.Close()
	}
}

// logf writes to the log file and, when verbose, to the terminal.
func (a *App) logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if a.logFile != nil {
		fmt.Fprintf(a.logFile, "%s %s\n", time.Now().Format(time.RFC3339), msg)
	}
	if a.Verbose {
		fmt.Fprintln(a.Out, "  ·", msg)
	}
}

func (a *App) say(format string, args ...any) { fmt.Fprintf(a.Out, format+"\n", args...) }

// ---------------------------------------------------------------- install

// Install sets up the whole stack. Safe to run repeatedly.
func (a *App) Install(ctx context.Context) error {
	a.say("Installing Hyper Stack runtime into %s", a.P.Root)
	spec := rt.StackSpec{PHPVersions: []string{defaultPHP}, NodeVersion: defaultNode}
	a.RT.Log = func(f string, args ...any) { a.say("  "+f, args...); a.logf(f, args...) }
	if err := a.RT.EnsureInstalled(ctx, spec); err != nil {
		return err
	}

	caPath, err := certs.EnsureCA(filepath.Join(a.P.Certs, "ca"))
	if err != nil {
		return err
	}
	if elevate.SetupNeeded() {
		a.say("Windows will ask for administrator permission once, to trust the local certificate and allow hosts-file edits.")
		if err := elevate.RunSetup(caPath); err != nil {
			return err
		}
		if elevate.SetupNeeded() {
			return fmt.Errorf("elevated setup finished but the CA or hosts permission is still missing: run hyper doctor")
		}
	}

	a.say("Preparing MySQL")
	if err := a.RT.EnsureMySQL(ctx); err != nil {
		return err
	}
	a.say("Done. Next: cd into a project with hyper.json and run: hyper up")
	return nil
}

func (a *App) requireInstalled() error {
	if _, err := os.Stat(filepath.Join(a.P.Bin, "nginx", ".hyper-installed")); err != nil {
		return fmt.Errorf("Hyper Stack is not installed on this machine. Run: hyper install")
	}
	return nil
}

// ---------------------------------------------------------------- sites

// Site is a registered project.
type Site struct {
	Name      string   `json:"name"`
	Domain    string   `json:"domain"`
	Root      string   `json:"root"`
	PHP       string   `json:"php"`
	Database  string   `json:"database"`
	Workers   []string `json:"workers"`
	SetupDone bool     `json:"setup_done"`
}

func (a *App) siteFile(name string) string { return filepath.Join(a.P.Sites, name+".json") }

func (a *App) saveSite(s Site) error {
	b, _ := json.MarshalIndent(s, "", "  ")
	return os.WriteFile(a.siteFile(s.Name), b, 0o644)
}

func (a *App) loadSite(name string) (Site, bool) {
	var s Site
	b, err := os.ReadFile(a.siteFile(name))
	if err != nil || json.Unmarshal(b, &s) != nil {
		return s, false
	}
	return s, true
}

func (a *App) sites() []Site {
	var out []Site
	entries, _ := os.ReadDir(a.P.Sites)
	for _, e := range entries {
		if n := strings.TrimSuffix(e.Name(), ".json"); n != e.Name() {
			if s, ok := a.loadSite(n); ok {
				out = append(out, s)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ---------------------------------------------------------------- up

// Up brings the project in dir online.
func (a *App) Up(ctx context.Context, dir string) error {
	cfgPath := filepath.Join(dir, "hyper.json")
	cfg, err := project.Load(cfgPath)
	if err != nil {
		return err
	}
	if err := a.requireInstalled(); err != nil {
		return err
	}
	root, _ := filepath.Abs(dir)
	a.say("Starting %s (%s, PHP %s)", cfg.Name, cfg.Domain, cfg.PHP)
	for _, s := range cfg.Services {
		a.say("  ! service %q is not supported yet and was skipped", s)
	}
	a.RT.Log = a.logf

	if err := a.checkPorts(); err != nil {
		return err
	}
	if _, err := a.RT.EnsurePHP(ctx, cfg.PHP, cfg.Extensions); err != nil {
		return err
	}
	if err := a.RT.EnsureMySQL(ctx); err != nil {
		return err
	}
	created, err := a.RT.CreateDatabase(ctx, cfg.Database.Name)
	if err != nil {
		return err
	}
	if created {
		a.say("  created database %s", cfg.Database.Name)
	}

	caDir := filepath.Join(a.P.Certs, "ca")
	cert, key, err := certs.EnsureLeaf(caDir, filepath.Join(a.P.Certs, "sites"), cfg.Domain)
	if err != nil {
		return err
	}

	prev, _ := a.loadSite(cfg.Name)
	site := Site{Name: cfg.Name, Domain: cfg.Domain, Root: root, PHP: cfg.PHP, Database: cfg.Database.Name,
		Workers: cfg.Workers, SetupDone: prev.SetupDone}
	if err := a.saveSite(site); err != nil {
		return err
	}
	if err := a.syncHosts(); err != nil {
		return err
	}
	if err := a.ensurePMA(ctx); err != nil {
		return err
	}
	if err := a.RT.RegisterSite(ctx, rt.Site{Name: cfg.Name, Domain: cfg.Domain, Root: root, PHP: cfg.PHP, CertPEM: cert, KeyPEM: key}); err != nil {
		return err
	}
	if err := a.RT.StartService(ctx, direct.PHPService(cfg.PHP)); err != nil {
		return err
	}
	if err := a.RT.StartService(ctx, rt.Nginx); err != nil {
		return err
	}
	if err := a.RT.ReloadWeb(ctx); err != nil {
		return err
	}

	pw, err := secrets.MySQLRoot()
	if err != nil {
		return err
	}
	err = envfile.UpdateFile(root, map[string]string{
		"APP_URL": a.S.SiteURL(cfg.Domain), "DB_CONNECTION": "mysql", "DB_HOST": "127.0.0.1",
		"DB_PORT": fmt.Sprint(a.S.MySQLPort), "DB_DATABASE": cfg.Database.Name, "DB_USERNAME": "root", "DB_PASSWORD": pw,
	})
	if err != nil {
		return err
	}

	if !site.SetupDone && len(cfg.Setup) > 0 {
		a.say("Running first-time setup")
		for _, c := range cfg.Setup {
			a.say("  $ %s", c)
			if err := a.runShell(ctx, site, c); err != nil {
				return fmt.Errorf("setup step failed: %q: %w. Fix it, then run hyper up again (setup resumes from the start)", c, err)
			}
		}
	}
	site.SetupDone = true
	if err := a.saveSite(site); err != nil {
		return err
	}

	for i, w := range cfg.Workers {
		if err := process.Start(a.workerSpec(site, i, w)); err != nil {
			return err
		}
		a.say("  worker started: %s", w)
	}
	if err := a.waitSite(ctx, cfg.Domain); err != nil {
		return err
	}
	a.say("Ready: %s", a.S.SiteURL(cfg.Domain))
	return nil
}

// waitSite waits until nginx serves the domain's own certificate (a reload
// swaps workers asynchronously, so the vhost is not live the instant it returns).
func (a *App) waitSite(ctx context.Context, domain string) error {
	addr := fmt.Sprintf("127.0.0.1:%d", a.S.HTTPSPort)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		d := tls.Dialer{Config: &tls.Config{ServerName: domain, InsecureSkipVerify: true}} //nolint:gosec // only inspecting which cert is served
		if c, err := d.DialContext(ctx, "tcp", addr); err == nil {
			state := c.(*tls.Conn).ConnectionState()
			c.Close()
			if len(state.PeerCertificates) > 0 && state.PeerCertificates[0].VerifyHostname(domain) == nil {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return fmt.Errorf("nginx did not start serving %s within 15s. Check %s", domain, filepath.Join(a.P.Logs, "nginx-error.log"))
}

func (a *App) syncHosts() error {
	domains := []string{pmaDomain}
	for _, s := range a.sites() {
		domains = append(domains, s.Domain)
	}
	if !elevate.HostsWritable() {
		return fmt.Errorf("cannot edit the hosts file %s. Run: hyper doctor --fix", elevate.HostsPath())
	}
	changed, err := hosts.Update(elevate.HostsPath(), a.hostsBackup(), domains)
	if err != nil {
		return err
	}
	if changed {
		a.logf("hosts block updated: %v", domains)
	}
	return nil
}

// ensurePMA registers phpMyAdmin as a site.
func (a *App) ensurePMA(ctx context.Context) error {
	cfg, err := templates.Raw("pma.config.inc.php")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(a.RT.PMADir(), "config.inc.php"), []byte(cfg), 0o644); err != nil {
		return fmt.Errorf("phpMyAdmin is not installed: run hyper install (%w)", err)
	}
	if _, err := a.RT.EnsurePHP(ctx, pmaPHP, nil); err != nil {
		return err
	}
	cert, key, err := certs.EnsureLeaf(filepath.Join(a.P.Certs, "ca"), filepath.Join(a.P.Certs, "sites"), pmaDomain)
	if err != nil {
		return err
	}
	if err := a.RT.RegisterSite(ctx, rt.Site{Name: "phpmyadmin", Domain: pmaDomain, Root: a.RT.PMADir(), WebRoot: a.RT.PMADir(),
		PHP: pmaPHP, CertPEM: cert, KeyPEM: key}); err != nil {
		return err
	}
	return a.RT.StartService(ctx, direct.PHPService(pmaPHP))
}

func (a *App) checkPorts() error {
	type p struct {
		port int
		svc  rt.Service
		what string
	}
	for _, c := range []p{{a.S.HTTPPort, rt.Nginx, "http_port"}, {a.S.HTTPSPort, rt.Nginx, "https_port"}, {a.S.MySQLPort, rt.MySQL, "mysql_port"}} {
		o, busy := doctor.PortOwner(c.port)
		if !busy {
			continue
		}
		if a.ownsProcess(o) {
			continue
		}
		return fmt.Errorf("port %d (%s) is already used by %s (pid %d, %s).\n  %s",
			c.port, c.what, o.Name, o.PID, o.Path, doctor.Hint(o, c.port))
	}
	return nil
}

// ---------------------------------------------------------------- shell & workers

func (a *App) shellEnv(s Site) []string {
	composerDir := filepath.Join(a.P.Run, "shims", s.PHP)
	_ = os.MkdirAll(composerDir, 0o755)
	shim := "@echo off\r\n\"" + a.RT.PHPExe(s.PHP) + "\" \"" + a.RT.ComposerPhar() + "\" %*\r\n"
	_ = os.WriteFile(filepath.Join(composerDir, "composer.bat"), []byte(shim), 0o644)
	path := strings.Join([]string{filepath.Dir(a.RT.PHPExe(s.PHP)), a.RT.NodeDir(), composerDir, os.Getenv("PATH")}, string(os.PathListSeparator))
	return append(os.Environ(), "PATH="+path, "COMPOSER_HOME="+filepath.Join(a.P.Root, "composer-home"))
}

func (a *App) runShell(ctx context.Context, s Site, command string) error {
	cmd := exec.CommandContext(ctx, "cmd", "/C", command)
	cmd.Dir = s.Root
	cmd.Env = a.shellEnv(s)
	cmd.Stdout, cmd.Stderr = a.Out, a.Out
	return cmd.Run()
}

func (a *App) workerSpec(s Site, i int, command string) process.Spec {
	return process.Spec{
		Name: workerName(s.Name, i), Bin: "cmd", Args: []string{"/C", command}, Dir: s.Root,
		Env: envDelta(a.shellEnv(s)), RunDir: a.P.Run, LogFile: filepath.Join(a.P.Logs, fmt.Sprintf("%s-worker-%d.log", s.Name, i)),
	}
}

// envDelta keeps only PATH and COMPOSER_HOME (process.Start appends to os.Environ).
func envDelta(env []string) []string {
	var out []string
	for _, e := range env {
		if strings.HasPrefix(strings.ToUpper(e), "PATH=") || strings.HasPrefix(e, "COMPOSER_HOME=") {
			out = append(out, e)
		}
	}
	return out
}

// ---------------------------------------------------------------- down

// Down stops the project's workers; with all, also the shared services.
func (a *App) Down(ctx context.Context, dir string, all bool) error {
	if dir != "" {
		if cfg, err := project.Load(filepath.Join(dir, "hyper.json")); err == nil {
			if s, ok := a.loadSite(cfg.Name); ok {
				for i, w := range s.Workers {
					if err := process.Stop(a.workerSpec(s, i, w)); err != nil {
						return err
					}
				}
				a.say("Stopped workers for %s", s.Name)
			}
		} else if !all {
			return err
		}
	}
	if all {
		for _, s := range a.sites() {
			for i, w := range s.Workers {
				_ = process.Stop(a.workerSpec(s, i, w))
			}
		}
		if err := a.RT.StopService(ctx, rt.Nginx); err != nil {
			return err
		}
		entries, _ := os.ReadDir(filepath.Join(a.P.Bin, "php"))
		for _, e := range entries {
			if e.IsDir() {
				if err := a.RT.StopService(ctx, direct.PHPService(e.Name())); err != nil {
					return err
				}
			}
		}
		if err := a.RT.StopMySQL(ctx); err != nil {
			return err
		}
		a.say("Stopped nginx, PHP and MySQL")
	}
	return nil
}

// ---------------------------------------------------------------- status

// Status prints services and registered projects.
func (a *App) Status(ctx context.Context) error {
	tw := tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SERVICE\tSTATE\tPORT\tPID")
	svcs := []rt.Service{rt.Nginx, rt.MySQL}
	entries, _ := os.ReadDir(filepath.Join(a.P.Bin, "php"))
	for _, e := range entries {
		if e.IsDir() {
			svcs = append(svcs, direct.PHPService(e.Name()))
		}
	}
	for _, s := range svcs {
		st, err := a.RT.ServiceStatus(ctx, s)
		if err != nil {
			continue
		}
		state := "stopped"
		if st.Running {
			state = "running"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\n", s, state, st.Port, st.PID)
	}
	tw.Flush()
	fmt.Fprintln(a.Out)
	tw = tabwriter.NewWriter(a.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PROJECT\tURL\tPHP\tDATABASE\tPATH")
	for _, s := range a.sites() {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", s.Name, a.S.SiteURL(s.Domain), s.PHP, s.Database, s.Root)
	}
	return tw.Flush()
}

// ---------------------------------------------------------------- db

// DBOpen opens phpMyAdmin on the project's database.
func (a *App) DBOpen(ctx context.Context, dir string) error {
	cfg, err := project.Load(filepath.Join(dir, "hyper.json"))
	if err != nil {
		return err
	}
	if err := a.requireInstalled(); err != nil {
		return err
	}
	a.RT.Log = a.logf
	if err := a.RT.EnsureMySQL(ctx); err != nil {
		return err
	}
	if err := a.syncHosts(); err != nil {
		return err
	}
	if err := a.ensurePMA(ctx); err != nil {
		return err
	}
	if err := a.RT.StartService(ctx, rt.Nginx); err != nil {
		return err
	}
	if err := a.RT.ReloadWeb(ctx); err != nil {
		return err
	}
	url := a.S.SiteURL(pmaDomain) + "/index.php?route=/database/structure&db=" + cfg.Database.Name
	a.say("Opening %s", url)
	return openBrowser(url)
}

func openBrowser(url string) error {
	if runtime.GOOS == "windows" {
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
	return exec.Command("open", url).Start()
}

// ---------------------------------------------------------------- config

// SetConfig changes a port setting.
func (a *App) SetConfig(key, value string) error {
	var n int
	if _, err := fmt.Sscanf(value, "%d", &n); err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("%q is not a valid port", value)
	}
	switch key {
	case "http_port":
		a.S.HTTPPort = n
	case "https_port":
		a.S.HTTPSPort = n
	case "mysql_port":
		a.S.MySQLPort = n
	default:
		return fmt.Errorf("unknown setting %q (use http_port, https_port or mysql_port)", key)
	}
	if err := a.P.SaveSettings(a.S); err != nil {
		return err
	}
	a.say("%s = %d. Run hyper down --all, then hyper up to apply.", key, n)
	return nil
}

// ---------------------------------------------------------------- uninstall

// Uninstall stops everything and removes system changes. With purge it also
// deletes the data directory and the stored MySQL password.
func (a *App) Uninstall(ctx context.Context, purge bool) error {
	_ = a.Down(ctx, "", true)
	if elevate.HostsWritable() {
		if _, err := hosts.Update(elevate.HostsPath(), a.hostsBackup(), nil); err != nil {
			return err
		}
		a.say("Removed the Hyper Stack block from the hosts file")
	}
	if elevate.CATrusted() || elevate.HostsWritable() {
		a.say("Windows will ask for administrator permission to remove the certificate trust and hosts permission.")
		if err := elevate.RunTeardown(); err != nil {
			return err
		}
	}
	appDir := filepath.Join(a.P.Root, "app")
	_ = process.Stop(process.Spec{Name: "ui", RunDir: a.P.Run}) // the dashboard, if running
	_ = os.Remove(filepath.Join(a.P.Run, "ui.json"))
	if err := selfinstall.Remove(appDir); err != nil {
		return err
	}
	a.say("Removed hyper from PATH, the Start menu and login autostart")
	if purge {
		if err := secrets.Delete(); err != nil {
			return err
		}
		a.Close()
		return purgeDataDir(a.P.Root, a.say)
	}
	return nil
}

// purgeDataDir deletes the data directory. The running hyper.exe lives inside
// it and Windows cannot delete a running exe, so everything else goes now and
// the rest is removed by a helper shortly after this process exits.
func purgeDataDir(root string, say func(string, ...any)) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	exe, _ := os.Executable()
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		if e.IsDir() && strings.HasPrefix(strings.ToLower(exe), strings.ToLower(p)+string(filepath.Separator)) {
			continue // holds the running exe
		}
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("cannot delete %s: %w", p, err)
		}
	}
	if left, _ := os.ReadDir(root); len(left) == 0 {
		_ = os.Remove(root)
		say("Deleted %s", root)
		return nil
	}
	cmd := exec.Command("cmd", "/C", fmt.Sprintf(`ping -n 4 127.0.0.1 >nul & rmdir /S /Q "%s"`, root))
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("could not schedule removal of %s: delete it manually after closing this window", root)
	}
	_ = cmd.Process.Release()
	say("Deleting %s (finishing in a few seconds)", root)
	return nil
}

// ownsProcess reports whether a listener is one of our own binaries (Windows
// mysqld and php-cgi run as a launcher plus a child, so pids alone are not enough).
func (a *App) ownsProcess(o doctor.Owner) bool {
	return o.Path != "" && strings.HasPrefix(strings.ToLower(o.Path), strings.ToLower(a.P.Root))
}

func (a *App) hostsBackup() string { return filepath.Join(a.P.Root, "hosts.hyper-backup") }
