// Package runtime defines what Hyper Stack needs from whatever runs PHP, Nginx
// and MySQL. Everything runtime-specific lives in a subpackage.
package runtime

import "context"

// Service names a managed background service.
type Service string

const (
	Nginx   Service = "nginx"
	MySQL   Service = "mysql"
	Redis   Service = "redis"
	Mailpit Service = "mailpit"
)

// Status is a service state.
type Status struct {
	Running bool
	PID     int
	Port    int
}

// StackSpec lists what hyper install must provide.
type StackSpec struct {
	PHPVersions []string
	NodeVersion string
}

// PHPInstall describes an installed PHP version.
type PHPInstall struct {
	Version string
	Binary  string // php executable
	FPMAddr string // host:port of its FPM (or php-cgi) listener
}

// Site is a project to serve.
type Site struct {
	Name    string
	Domain  string
	Root    string
	WebRoot string // optional; defaults to Root/public
	PHP     string
	CertPEM string
	KeyPEM  string
}

// RuntimePaths are the directories doctor and logging need.
type RuntimePaths struct {
	Data, Logs, Config string
}

// Runtime is the only seam between Hyper Stack and the process manager.
type Runtime interface {
	Name() string
	EnsureInstalled(ctx context.Context, spec StackSpec) error
	EnsurePHP(ctx context.Context, version string, extensions []string) (PHPInstall, error)
	StartService(ctx context.Context, svc Service) error
	StopService(ctx context.Context, svc Service) error
	ServiceStatus(ctx context.Context, svc Service) (Status, error)
	RegisterSite(ctx context.Context, site Site) error
	UnregisterSite(ctx context.Context, name string) error
	ReloadWeb(ctx context.Context) error
	Paths() RuntimePaths
}
