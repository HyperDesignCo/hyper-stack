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
	logDir := p.Logs
	if p.Configured {
		if err := p.Ensure(); err != nil {
			return nil, err
		}
	} else {
		// No install folder chosen yet: only the fixed home exists.
		logDir = p.HomeLogs
		for _, d := range []string{p.Home, p.HomeRun, p.HomeLogs} {
			if err := os.MkdirAll(d, 0o755); err != nil {
				return nil, err
			}
		}
	}
	s, err := p.LoadSettings()
	if err != nil {
		return nil, err
	}
	a := &App{P: p, S: s, Out: out, Verbose: verbose}
	a.logFile, _ = os.OpenFile(filepath.Join(logDir, "hyper.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	a.RT = direct.New(p, s, a.logf)
	return a, nil
}

// retarget points the app at a (newly chosen) install folder.
func (a *App) retarget(p config.Paths) error {
	if err := p.Ensure(); err != nil {
		return err
	}
	s, err := p.LoadSettings()
	if err != nil {
		return err
	}
	if a.logFile != nil {
		_ = a.logFile.Close()
	}
	a.logFile, _ = os.OpenFile(filepath.Join(p.Logs, "hyper.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	a.P, a.S = p, s
	a.RT = direct.New(p, s, a.logf)
	return nil
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

// InstallTo installs into dir (the folder the user chose, like C:\laragon).
// With an install already in place dir may be empty. Moving an existing
// install to another folder is not supported: uninstall it first.
// Ports optionally overrides the default 80 / 443 / 3306 (zero keeps the current value).
type Ports struct{ HTTP, HTTPS, MySQL int }

func (a *App) InstallTo(ctx context.Context, dir string, ports Ports) error {
	if a.P.Configured {
		if dir != "" {
			want, err := config.ValidateInstallDir(dir)
			if err != nil {
				return err
			}
			if !strings.EqualFold(filepath.Clean(want), filepath.Clean(a.P.Root)) {
				return fmt.Errorf("the install already lives in %s. To use a different folder run: hyper uninstall --purge, then install again", a.P.Root)
			}
		}
		if err := a.applyPorts(ports); err != nil {
			return err
		}
		return a.Install(ctx)
	}
	if dir == "" {
		return fmt.Errorf("choose an install folder, for example: hyper install --dir %s", config.DefaultInstallDir())
	}
	abs, err := config.ValidateInstallDir(dir)
	if err != nil {
		return err
	}
	cand := a.S
	ports.applyTo(&cand)
	if err := a.preflightPorts(cand); err != nil {
		return err // nothing has been created or saved yet
	}
	p, err := config.ForRoot(abs)
	if err != nil {
		return err
	}
	if err := a.retarget(p); err != nil {
		return err
	}
	if err := config.SaveRoot(abs); err != nil {
		return err
	}
	if err := a.applyPorts(ports); err != nil {
		return err
	}
	return a.Install(ctx)
}

func (p Ports) applyTo(s *config.Settings) {
	for _, c := range []struct {
		v   int
		dst *int
	}{{p.HTTP, &s.HTTPPort}, {p.HTTPS, &s.HTTPSPort}, {p.MySQL, &s.MySQLPort}} {
		if c.v > 0 && c.v <= 65535 {
			*c.dst = c.v
		}
	}
}

func (a *App) applyPorts(p Ports) error {
	for _, v := range []int{p.HTTP, p.HTTPS, p.MySQL} {
		if v < 0 || v > 65535 {
			return fmt.Errorf("%d is not a valid port", v)
		}
	}
	before := a.S
	p.applyTo(&a.S)
	if a.S.HTTPPort == before.HTTPPort && a.S.HTTPSPort == before.HTTPSPort && a.S.MySQLPort == before.MySQLPort {
		return nil
	}
	if err := a.P.SaveSettings(a.S); err != nil {
		return err
	}
	a.RT = direct.New(a.P, a.S, a.logf)
	return nil
}

// preflightPorts runs before any download. MySQL starts during install, so its
// port must be free (this is where a running Laragon shows up); the web ports
// only matter once a project starts, so they are a warning.
func (a *App) preflightPorts(st config.Settings) error {
	for _, c := range []struct {
		port int
		key  string
		must bool
	}{{st.MySQLPort, "mysql_port", true}, {st.HTTPPort, "http_port", false}, {st.HTTPSPort, "https_port", false}} {
		o, busy := doctor.PortOwner(c.port)
		if !busy || a.ownsProcess(o) {
			continue
		}
		msg := fmt.Sprintf("port %d (%s) is used by %s (%s).\n  %s", c.port, c.key, o.Name, o.Path, doctor.Hint(o, c.port))
		if c.must {
			return fmt.Errorf("%s\n  Or install next to it on other ports: hyper install --dir <folder> --mysql-port 3307 --http-port 8080 --https-port 8443", msg)
		}
		a.say("  ! %s\n    (this only matters when you start a project)", msg)
	}
	return nil
}

// Install sets up the whole stack. Safe to run repeatedly.
func (a *App) Install(ctx context.Context) error {
	a.say("Installing Hyper Stack into %s", a.P.Root)
	if err := a.preflightPorts(a.S); err != nil {
		return err
	}
	spec := rt.StackSpec{PHPVersions: []string{defaultPHP}, NodeVersion: defaultNode}
	a.RT.Log = func(f string, args ...any) { a.say("  "+f, args...); a.logf(f, args...) }
	if err := a.RT.EnsureInstalled(ctx, spec); err != nil {
		return err
	}

	caPath, err := certs.EnsureCA(filepath.Join(a.P.Certs, "ca"))
	if err != nil {
		return err
	}
	if elevate.SetupNeeded(caPath) {
		a.say("%s once, to trust the local certificate and allow hosts-file edits.", elevate.AdminPrompt())
		if err := elevate.RunSetup(caPath); err != nil {
			return err
		}
		if elevate.SetupNeeded(caPath) {
			return fmt.Errorf("elevated setup finished but the CA or hosts permission is still missing: run hyper doctor")
		}
	}

	a.say("Preparing MySQL")
	if err := a.RT.EnsureMySQL(ctx); err != nil {
		return err
	}
	a.say("Done. Put your projects in %s (each folder becomes <folder>.test), then run: hyper up --all", a.P.WWW)
	return nil
}

func (a *App) requireInstalled() error {
	if _, err := os.Stat(filepath.Join(a.P.Bin, a.RT.WebName(), ".hyper-installed")); err != nil {
		return fmt.Errorf("the Hyper Stack runtime is not installed on this machine. Run: hyper install")
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
	Auto      bool     `json:"auto,omitempty"` // configured from the folder, no hyper.json
	WebRoot   string   `json:"web_root,omitempty"`
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
	root, _ := filepath.Abs(dir)
	cfg, auto, err := project.Resolve(root)
	if err != nil {
		return err
	}
	if err := a.requireInstalled(); err != nil {
		return err
	}
	if other, ok := a.loadSite(cfg.Name); ok && !strings.EqualFold(filepath.Clean(other.Root), filepath.Clean(root)) {
		if _, statErr := os.Stat(other.Root); statErr == nil {
			return fmt.Errorf("the name %q is already used by the project in %s. Rename one of the folders (or set a different \"name\" in hyper.json)", cfg.Name, other.Root)
		}
	}
	webRoot := project.WebRoot(root)
	if auto {
		a.say("Starting %s (%s, PHP %s, detected from the folder)", cfg.Name, cfg.Domain, cfg.PHP)
	} else {
		a.say("Starting %s (%s, PHP %s)", cfg.Name, cfg.Domain, cfg.PHP)
	}
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
		Workers: cfg.Workers, SetupDone: prev.SetupDone, Auto: auto, WebRoot: webRoot}
	if err := a.saveSite(site); err != nil {
		return err
	}
	if err := a.syncHosts(); err != nil {
		return err
	}
	if err := a.ensurePMA(ctx); err != nil {
		return err
	}
	if err := a.RT.RegisterSite(ctx, rt.Site{Name: cfg.Name, Domain: cfg.Domain, Root: root, WebRoot: webRoot, PHP: cfg.PHP, CertPEM: cert, KeyPEM: key}); err != nil {
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

	pw := secrets.RootPassword()
	// Plain PHP/WordPress-style folders have no Laravel .env to manage.
	if project.IsLaravelLike(root) || (!auto && hasEnvFile(root)) {
		err = envfile.UpdateFile(root, map[string]string{
			"APP_URL": a.S.SiteURL(cfg.Domain), "DB_CONNECTION": "mysql", "DB_HOST": "127.0.0.1",
			"DB_PORT": fmt.Sprint(a.S.MySQLPort), "DB_DATABASE": cfg.Database.Name, "DB_USERNAME": "root", "DB_PASSWORD": pw,
		})
		if err != nil {
			return err
		}
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
		// Every attempt is bounded: a server that accepts the connection but never answers
		// must not keep the start button "working" forever.
		try, cancel := context.WithTimeout(ctx, 3*time.Second)
		d := tls.Dialer{Config: &tls.Config{ServerName: domain, InsecureSkipVerify: true}} //nolint:gosec // only inspecting which cert is served
		c, err := d.DialContext(try, "tcp", addr)
		cancel()
		if err == nil {
			state := c.(*tls.Conn).ConnectionState()
			_ = c.Close()
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

// ---------------------------------------------------------------- down

// Down stops the project's workers; with all, also the shared services.
func (a *App) Down(ctx context.Context, dir string, all bool) error {
	if dir != "" {
		if cfg, _, err := project.Resolve(dir); err == nil {
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
	_ = tw.Flush()
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
	cfg, _, err := project.Resolve(dir)
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
	if elevate.CATrustedByName() || elevate.HostsWritable() {
		a.say("%s to remove the certificate trust and hosts permission.", elevate.AdminPrompt())
		if err := elevate.RunTeardown(); err != nil {
			return err
		}
	}
	appDir, err := config.AppDir()
	if err != nil {
		return err
	}
	_ = process.Stop(process.Spec{Name: "ui", RunDir: a.P.HomeRun, NoTree: true}) // the dashboard, if running
	_ = os.Remove(filepath.Join(a.P.HomeRun, "ui.json"))
	if err := selfinstall.Remove(appDir); err != nil {
		return err
	}
	a.say("Removed hyper from PATH, the Start menu and login autostart")
	if purge {
		if err := secrets.Delete(); err != nil {
			return err
		}
		a.Close()
		if err := purgeDataDir(a.P.Root, a.say); err != nil {
			return err
		}
		return config.ClearRoot()
	}
	return nil
}

// purgeDataDir deletes everything Hyper Stack installed (runtimes, databases,
// certificates, logs) but NEVER the www folder: that is where the user's
// projects live. A folder holding the running hyper.exe (older installs kept it
// inside the data folder) cannot be deleted on Windows while it runs, so it is
// removed by a helper just after this process exits.
func purgeDataDir(root string, say func(string, ...any)) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	exe, _ := os.Executable()
	var later []string
	kept := false
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		if strings.EqualFold(e.Name(), "www") {
			kept = true
			continue
		}
		if e.IsDir() && strings.HasPrefix(strings.ToLower(exe), strings.ToLower(p)+string(filepath.Separator)) {
			later = append(later, p)
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			return fmt.Errorf("cannot delete %s: %w", p, err)
		}
	}
	for _, p := range later {
		if runtime.GOOS != "windows" {
			// only Windows cannot delete a program that is running
			_ = os.RemoveAll(p)
			continue
		}
		cmd := exec.Command("cmd", "/C", fmt.Sprintf(`ping -n 4 127.0.0.1 >nul & rmdir /S /Q "%s"`, p))
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("could not schedule removal of %s: delete it manually after closing this window", p)
		}
		_ = cmd.Process.Release()
	}
	if kept {
		say("Deleted the Hyper Stack runtime and databases. Your projects were NOT touched: %s", filepath.Join(root, "www"))
		return nil
	}
	if len(later) == 0 {
		_ = os.Remove(root)
	}
	say("Deleted %s", root)
	return nil
}

// ownsProcess reports whether a listener is one of our own binaries (Windows
// mysqld and php-cgi run as a launcher plus a child, so pids alone are not enough).
func (a *App) ownsProcess(o doctor.Owner) bool {
	return o.Path != "" && strings.HasPrefix(strings.ToLower(o.Path), strings.ToLower(a.P.Root))
}

func (a *App) hostsBackup() string { return filepath.Join(a.P.Root, "hosts.hyper-backup") }

// hasEnvFile reports whether the project has a .env (or a .env.example to start from).
func hasEnvFile(dir string) bool {
	for _, f := range []string{".env", ".env.example"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}
