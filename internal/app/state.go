package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/hyperdesign/hyper-stack/internal/config"
	"github.com/hyperdesign/hyper-stack/internal/elevate"
	"github.com/hyperdesign/hyper-stack/internal/process"
	"github.com/hyperdesign/hyper-stack/internal/project"
	rt "github.com/hyperdesign/hyper-stack/internal/runtime"
	"github.com/hyperdesign/hyper-stack/internal/runtime/direct"
)

// ServiceInfo is a shared service's state.
type ServiceInfo struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Port    int    `json:"port"`
	PID     int    `json:"pid"`
}

// WorkerInfo is one background worker of a project.
type WorkerInfo struct {
	Command string `json:"command"`
	Running bool   `json:"running"`
}

// SiteInfo is a registered project's state.
type SiteInfo struct {
	Name       string       `json:"name"`
	Domain     string       `json:"domain"`
	URL        string       `json:"url"`
	Root       string       `json:"root"`
	PHP        string       `json:"php"`
	Database   string       `json:"database"`
	Online     bool         `json:"online"` // vhost registered and nginx serving
	RootExists bool         `json:"root_exists"`
	SetupDone  bool         `json:"setup_done"`
	Workers    []WorkerInfo `json:"workers"`
}

// State is everything the dashboard shows.
type State struct {
	Installed   bool            `json:"installed"`
	DataDir     string          `json:"data_dir"`
	Settings    config.Settings `json:"settings"`
	HostsWrite  bool            `json:"hosts_writable"`
	Services    []ServiceInfo   `json:"services"`
	Sites       []SiteInfo      `json:"sites"`
	PMAURL      string          `json:"phpmyadmin_url"`
	AnyRunning  bool            `json:"any_running"`
	LogFiles    []string        `json:"log_files"`
	HyperVerStr string          `json:"version,omitempty"`
}

// State gathers the current state. It has no side effects.
func (a *App) State(ctx context.Context) State {
	st := State{
		DataDir: a.P.Root, Settings: a.S, HostsWrite: elevate.HostsWritable(),
		PMAURL:    a.S.SiteURL(pmaDomain),
		Services:  []ServiceInfo{},
		Sites:     []SiteInfo{},
		LogFiles:  a.logFiles(),
		Installed: a.requireInstalled() == nil,
	}
	svcs := []rt.Service{rt.Nginx, rt.MySQL}
	entries, _ := os.ReadDir(filepath.Join(a.P.Bin, "php"))
	for _, e := range entries {
		if e.IsDir() && !strings.HasSuffix(e.Name(), ".tmp") {
			svcs = append(svcs, direct.PHPService(e.Name()))
		}
	}
	nginxUp := false
	for _, s := range svcs {
		if info, err := a.RT.ServiceStatus(ctx, s); err == nil {
			st.Services = append(st.Services, ServiceInfo{Name: string(s), Running: info.Running, Port: info.Port, PID: info.PID})
			if info.Running {
				st.AnyRunning = true
			}
			if s == rt.Nginx && info.Running {
				nginxUp = true
			}
		}
	}
	for _, s := range a.sites() {
		si := SiteInfo{Name: s.Name, Domain: s.Domain, URL: a.S.SiteURL(s.Domain), Root: s.Root, PHP: s.PHP,
			Database: s.Database, SetupDone: s.SetupDone, Workers: []WorkerInfo{}}
		_, err := os.Stat(s.Root)
		si.RootExists = err == nil
		si.Online = nginxUp && a.RT.SiteRegistered(s.Name)
		for i, w := range s.Workers {
			si.Workers = append(si.Workers, WorkerInfo{Command: w, Running: process.Running(process.Spec{Name: workerName(s.Name, i), RunDir: a.P.Run})})
		}
		st.Sites = append(st.Sites, si)
	}
	return st
}

func workerName(site string, i int) string { return fmt.Sprintf("worker-%s-%d", site, i) }

// logFiles lists viewable log files (names only).
func (a *App) logFiles() []string {
	entries, _ := os.ReadDir(a.P.Logs)
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".log") {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

var safeLogName = regexp.MustCompile(`^[A-Za-z0-9._-]+\.log$`)

// TailLog returns the last n lines of a log file in the logs directory.
func (a *App) TailLog(name string, n int) (string, error) {
	if !safeLogName.MatchString(name) {
		return "", fmt.Errorf("invalid log name")
	}
	b, err := os.ReadFile(filepath.Join(a.P.Logs, name))
	if err != nil {
		return "", err
	}
	if len(b) > 512*1024 {
		b = b[len(b)-512*1024:]
	}
	lines := strings.Split(strings.TrimRight(string(b), "\r\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}

// Inspection describes what bringing a folder up would do.
type Inspection struct {
	Root         string          `json:"root"`
	Name         string          `json:"name"`
	Domain       string          `json:"domain"`
	PHP          string          `json:"php"`
	Database     string          `json:"database"`
	Extensions   []string        `json:"extensions"`
	Setup        []string        `json:"setup"`
	Workers      []string        `json:"workers"`
	Registered   bool            `json:"registered"`
	NeedsConfirm bool            `json:"needs_confirm"` // first-run setup commands must be reviewed
	Config       *project.Config `json:"-"`
}

// Inspect validates dir/hyper.json and reports what up would run.
func (a *App) Inspect(dir string) (*Inspection, error) {
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	cfg, err := project.Load(filepath.Join(root, "hyper.json"))
	if err != nil {
		return nil, err
	}
	prev, registered := a.loadSite(cfg.Name)
	return &Inspection{
		Root: root, Name: cfg.Name, Domain: cfg.Domain, PHP: cfg.PHP, Database: cfg.Database.Name,
		Extensions: orEmpty(cfg.Extensions), Setup: orEmpty(cfg.Setup), Workers: orEmpty(cfg.Workers),
		Registered: registered, NeedsConfirm: len(cfg.Setup) > 0 && !prev.SetupDone, Config: cfg,
	}, nil
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// StopSite takes a project offline: stops its workers and removes its vhost.
// The hosts entry and database stay, so Up brings it back.
func (a *App) StopSite(ctx context.Context, name string) error {
	s, ok := a.loadSite(name)
	if !ok {
		return fmt.Errorf("unknown project %q", name)
	}
	for i, w := range s.Workers {
		if err := process.Stop(a.workerSpec(s, i, w)); err != nil {
			return err
		}
	}
	if err := a.RT.UnregisterSite(ctx, name); err != nil {
		return err
	}
	if st, _ := a.RT.ServiceStatus(ctx, rt.Nginx); st.Running {
		if err := a.RT.ReloadWeb(ctx); err != nil {
			return err
		}
	}
	a.say("Stopped %s", name)
	return nil
}

// SiteRoot returns the folder of a registered project.
func (a *App) SiteRoot(name string) (string, bool) {
	s, ok := a.loadSite(name)
	return s.Root, ok
}

// SiteDB returns the database of a registered project.
func (a *App) SiteDB(name string) (string, bool) {
	s, ok := a.loadSite(name)
	return s.Database, ok
}

// ForgetSite removes a project from the registry (it must be stopped first).
func (a *App) ForgetSite(ctx context.Context, name string) error {
	if err := a.StopSite(ctx, name); err != nil {
		return err
	}
	if err := os.Remove(a.siteFile(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return a.syncHosts()
}
