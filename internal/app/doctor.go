package app

import (
	"compress/gzip"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hyperdesign/hyper-stack/internal/certs"
	"github.com/hyperdesign/hyper-stack/internal/doctor"
	"github.com/hyperdesign/hyper-stack/internal/elevate"
	"github.com/hyperdesign/hyper-stack/internal/hosts"
	"github.com/hyperdesign/hyper-stack/internal/project"
	rt "github.com/hyperdesign/hyper-stack/internal/runtime"
	"github.com/hyperdesign/hyper-stack/internal/secrets"
)

// Finding is one doctor check result.
type Finding struct {
	OK     bool   `json:"ok"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
}

// Doctor checks the environment. It only changes things when fix is true.
func (a *App) Doctor(ctx context.Context, fix bool) error {
	fs := a.Checks(ctx)
	return a.reportDoctor(ctx, fs, fix)
}

// Checks runs every doctor check without changing anything.
func (a *App) Checks(ctx context.Context) []Finding {
	var fs []Finding
	add := func(ok bool, title, detail, fixHint string) { fs = append(fs, Finding{ok, title, detail, fixHint}) }

	// Installation
	if err := a.requireInstalled(); err != nil {
		add(false, "Runtime not installed", err.Error(), "hyper install")
	} else {
		add(true, "Runtime installed", a.P.Root, "")
	}

	// Ports
	for _, c := range []struct {
		port int
		svc  rt.Service
		key  string
	}{{a.S.HTTPPort, rt.Nginx, "http_port"}, {a.S.HTTPSPort, rt.Nginx, "https_port"}, {a.S.MySQLPort, rt.MySQL, "mysql_port"}} {
		o, busy := doctor.PortOwner(c.port)
		if !busy {
			add(true, fmt.Sprintf("Port %d is free", c.port), c.key, "")
			continue
		}
		if a.ownsProcess(o) {
			add(true, fmt.Sprintf("Port %d is used by Hyper Stack", c.port), o.Name, "")
			continue
		}
		add(false, fmt.Sprintf("Port %d is used by %s (pid %d)", c.port, o.Name, o.PID), o.Path, doctor.Hint(o, c.port))
	}

	// Trust and hosts permission
	if elevate.CATrusted(filepath.Join(a.P.Certs, "ca", "rootCA.pem")) {
		add(true, "Local CA is trusted", elevate.CAName, "")
	} else {
		add(false, "Local CA is not trusted", "Browsers will warn about https://*.test", "hyper doctor --fix")
	}
	if elevate.HostsWritable() {
		add(true, "Hosts file is writable", elevate.HostsPath(), "")
	} else {
		add(false, "Hosts file is not writable", elevate.HostsPath(), "hyper doctor --fix")
	}

	// Hosts entries and certificates per project
	have := map[string]bool{}
	if b, err := os.ReadFile(elevate.HostsPath()); err == nil {
		for _, d := range hosts.Domains(string(b)) {
			have[d] = true
		}
	}
	for _, s := range a.sites() {
		if have[s.Domain] {
			add(true, "hosts entry for "+s.Domain, "", "")
		} else {
			add(false, "hosts entry missing for "+s.Domain, "", "hyper doctor --fix")
		}
		if days, err := certDaysLeft(filepath.Join(a.P.Certs, "sites", s.Domain+".pem")); err != nil {
			add(false, "Certificate for "+s.Domain+" unreadable", err.Error(), "hyper up (re-issues it)")
		} else if days < 30 {
			add(false, fmt.Sprintf("Certificate for %s expires in %d days", s.Domain, days), "", "delete the file in "+filepath.Join(a.P.Certs, "sites")+" and run hyper up")
		} else {
			add(true, fmt.Sprintf("Certificate for %s valid for %d days", s.Domain, days), "", "")
		}
		if _, err := os.Stat(s.Root); err != nil {
			add(false, "Project folder missing: "+s.Root, "", "remove "+a.siteFile(s.Name))
		}
	}

	return fs
}

func (a *App) reportDoctor(ctx context.Context, fs []Finding, fix bool) error {
	failed := 0
	for _, f := range fs {
		mark := "ok  "
		if !f.OK {
			mark = "FAIL"
			failed++
		}
		fmt.Fprintf(a.Out, "[%s] %s\n", mark, f.Title)
		if !f.OK {
			if f.Detail != "" {
				fmt.Fprintf(a.Out, "       %s\n", f.Detail)
			}
			if f.Fix != "" {
				fmt.Fprintf(a.Out, "       fix: %s\n", f.Fix)
			}
		}
	}

	if fix && failed > 0 {
		fmt.Fprintln(a.Out, "\nApplying safe fixes (trust, hosts permission, hosts entries)...")
		caPath, err := certs.EnsureCA(filepath.Join(a.P.Certs, "ca"))
		if err != nil {
			return err
		}
		if elevate.SetupNeeded(caPath) {
			if err := elevate.RunSetup(caPath); err != nil {
				return err
			}
		}
		if elevate.HostsWritable() {
			if err := a.syncHosts(); err != nil {
				return err
			}
		}
		fmt.Fprintln(a.Out, "Re-run hyper doctor to confirm. Port conflicts are never fixed automatically.")
		return nil
	}
	if failed > 0 {
		return fmt.Errorf("%d problem(s) found", failed)
	}
	return nil
}

func certDaysLeft(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	p, _ := pem.Decode(b)
	if p == nil {
		return 0, fmt.Errorf("not a PEM file")
	}
	c, err := x509.ParseCertificate(p.Bytes)
	if err != nil {
		return 0, err
	}
	return int(time.Until(c.NotAfter).Hours() / 24), nil
}

// DBImport loads a .sql or .sql.gz file into the project's database. With fresh,
// the database is emptied first (its current contents are saved to a backup
// file), which is what you want after an import that stopped half way.
func (a *App) DBImport(ctx context.Context, dir, file string, fresh bool) error {
	cfg, _, err := project.Resolve(dir)
	if err != nil {
		return err
	}
	if err := a.requireInstalled(); err != nil {
		return err
	}
	f, err := os.Open(file)
	if err != nil {
		return fmt.Errorf("cannot open %s: %w", file, err)
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return err
	}
	a.RT.Log = a.logf
	if err := a.RT.EnsureMySQL(ctx); err != nil {
		return err
	}
	db := cfg.Database.Name
	if fresh {
		if n, err := a.RT.TableCount(ctx, db); err == nil && n > 0 {
			backup := filepath.Join(a.P.Root, "backups", fmt.Sprintf("%s-%s.sql", db, time.Now().Format("20060102-150405")))
			a.say("Saving a backup of the %d existing table(s) first", n)
			if err := a.RT.BackupDatabase(ctx, db, backup); err != nil {
				return fmt.Errorf("not emptying %s because its backup failed: %w", db, err)
			}
			a.say("  backup: %s", backup)
		}
		if err := a.RT.ResetDatabase(ctx, db); err != nil {
			return err
		}
		a.say("Emptied database %s", db)
	} else if _, err := a.RT.CreateDatabase(ctx, db); err != nil {
		return err
	}

	a.say("Importing %s into %s", filepath.Base(file), db)
	pr := &progressReader{r: f, total: st.Size(), out: a.Out}
	var in io.Reader = pr
	if strings.HasSuffix(strings.ToLower(file), ".gz") {
		gz, err := gzip.NewReader(pr)
		if err != nil {
			return fmt.Errorf("%s is not a valid gzip file: %w", file, err)
		}
		defer gz.Close()
		in = gz
	}
	if err := a.RT.ImportSQL(ctx, db, in); err != nil {
		return fmt.Errorf("%w\n%s", err, importHint(err))
	}
	fmt.Fprintln(a.Out)
	a.say("Import finished")
	return nil
}

// importHint says what to do after a failed import. The file is applied statement
// by statement, so a failure part-way leaves what came before it in the database.
func importHint(err error) string {
	msg := err.Error()
	if strings.Contains(msg, "already exists") {
		return "The database already has these tables, probably from an earlier import. To start clean, import again with the option to replace the database (hyper db:import --fresh, or tick \"Replace the database\" in the dashboard). A backup of the old contents is saved first."
	}
	return "Part of the file may already be in the database. After fixing the cause, import again with the option to replace the database (hyper db:import --fresh, or tick \"Replace the database\" in the dashboard); a backup of the old contents is saved first."
}

// progressReader prints a percentage as the compressed/raw file is consumed.
type progressReader struct {
	r     io.Reader
	total int64
	done  int64
	last  int
	out   io.Writer
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.done += int64(n)
	if p.total > 0 {
		if pct := int(p.done * 100 / p.total); pct != p.last {
			p.last = pct
			fmt.Fprintf(p.out, "\r  %3d%%", pct)
		}
	}
	return n, err
}

// DBInfo prints the connection details for the local MySQL, for plain PHP
// projects that have no .env to fill in.
func (a *App) DBInfo() error {
	if err := a.requireInstalled(); err != nil {
		return err
	}
	pw := secrets.RootPassword()
	a.say("Host      127.0.0.1")
	a.say("Port      %d", a.S.MySQLPort)
	a.say("User      root")
	if pw == "" {
		a.say("Password  (none: leave it empty)")
	} else {
		a.say("Password  %s", pw)
	}
	a.say("phpMyAdmin %s", a.S.SiteURL(pmaDomain))
	return nil
}
