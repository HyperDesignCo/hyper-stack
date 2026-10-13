package direct

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/hyperdesign/hyper-stack/internal/process"
	rt "github.com/hyperdesign/hyper-stack/internal/runtime"
	"github.com/hyperdesign/hyper-stack/internal/secrets"
	"github.com/hyperdesign/hyper-stack/templates"
)

func (r *Runtime) myCnf() string { return filepath.Join(r.P.Run, "my.cnf") }

func (r *Runtime) writeMyCnf() error {
	if err := os.MkdirAll(r.P.Run, 0o755); err != nil {
		return err
	}
	cnf, err := templates.Render("my.cnf.tmpl", map[string]any{
		"Port": r.S.MySQLPort, "BaseDir": filepath.ToSlash(r.mysqlDir()), "DataDir": filepath.ToSlash(r.mysqlData()),
		"LogFile": filepath.ToSlash(filepath.Join(r.P.Logs, "mysql-error.log")), "Socket": r.mysqlSocket(),
	})
	if err != nil {
		return err
	}
	return os.WriteFile(r.myCnf(), []byte(cnf), 0o644)
}

// EnsureMySQL initializes the data directory, starts MySQL and makes sure the
// root password is the one stored in the credential store.
func (r *Runtime) EnsureMySQL(ctx context.Context) error {
	if err := r.requireSupported(); err != nil {
		return err
	}
	if err := r.writeMyCnf(); err != nil {
		return err
	}
	data := r.mysqlData()
	if _, err := os.Stat(filepath.Join(data, "mysql")); os.IsNotExist(err) {
		r.Log("Initializing MySQL data directory")
		if err := os.MkdirAll(filepath.Dir(data), 0o755); err != nil {
			return err
		}
		_ = os.RemoveAll(data)
		out, err := execCombined(r.mysqlBin("mysqld"), "",
			"--defaults-file="+r.myCnf(), "--initialize-insecure", "--console")
		if err != nil {
			_ = os.RemoveAll(data)
			return fmt.Errorf("MySQL initialization failed: %w\n%s", err, tail(out, 800))
		}
	}
	if err := r.StartService(ctx, rt.MySQL); err != nil {
		return err
	}

	// New installs use root with no password, like Laragon and XAMPP: MySQL only
	// listens on 127.0.0.1, and a password nobody can remember helps nobody.
	// An older install keeps the password it stored in the credential store.
	pw := secrets.RootPassword()
	if _, err := r.mysql(ctx, pw, "", "SELECT 1"); err != nil {
		if pw == "" {
			return fmt.Errorf("cannot log in to MySQL as root without a password: %w. "+
				"If this data folder was made by an older Hyper Stack, run hyper uninstall --purge and hyper install again", err)
		}
		// Older install, fresh data folder: root still has no password, so set the stored one.
		if _, err := r.mysql(ctx, "", "--skip-password", "ALTER USER 'root'@'localhost' IDENTIFIED BY '"+pw+"'"); err != nil {
			return fmt.Errorf("cannot set the MySQL root password and the stored one does not work: %w. "+
				"Run hyper uninstall --purge and hyper install again", err)
		}
	}
	return nil
}

// CreateDatabase creates the database if missing and reports whether it did.
func (r *Runtime) CreateDatabase(ctx context.Context, name string) (bool, error) {
	pw := secrets.RootPassword()
	out, err := r.mysql(ctx, pw, "", "SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='"+name+"'")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(lastLine(out)) != "0" {
		return false, nil
	}
	_, err = r.mysql(ctx, pw, "", "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
	return err == nil, err
}

// ImportSQL streams SQL into a database.
func (r *Runtime) ImportSQL(ctx context.Context, db string, in io.Reader) error {
	cmd := exec.CommandContext(ctx, r.mysqlBin("mysql"), importArgs(r.myCnf(), db)...)
	if pw := secrets.RootPassword(); pw != "" {
		cmd.Env = append(os.Environ(), "MYSQL_PWD="+pw)
	}
	cmd.Stdin = in
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("import failed: %w\n%s", err, tail(out.String(), 800))
	}
	return nil
}

// importArgs are the mysql client arguments for loading a dump. Foreign-key
// checks are off while loading, exactly as mysqldump itself restores: dumps put
// the constraints at the end, and real data often has rows that point at records
// that no longer exist, which would otherwise stop the import on the last step.
func importArgs(cnf, db string) []string {
	return []string{"--defaults-file=" + cnf, "-uroot", "--max-allowed-packet=64M", "--init-command=SET FOREIGN_KEY_CHECKS=0", db}
}

// TableCount returns how many tables a database holds.
func (r *Runtime) TableCount(ctx context.Context, db string) (int, error) {
	out, err := r.mysql(ctx, secrets.RootPassword(), "", "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='"+db+"'")
	if err != nil {
		return 0, err
	}
	var n int
	_, _ = fmt.Sscanf(strings.TrimSpace(lastLine(out)), "%d", &n)
	return n, nil
}

// BackupDatabase writes a dump of db to file with mysqldump.
func (r *Runtime) BackupDatabase(ctx context.Context, db, file string) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, r.mysqlBin("mysqldump"),
		"--defaults-file="+r.myCnf(), "-uroot", "--single-transaction", "--routines", "--triggers", "--no-tablespaces",
		"--default-character-set=utf8mb4", "--result-file="+file, db)
	if pw := secrets.RootPassword(); pw != "" {
		cmd.Env = append(os.Environ(), "MYSQL_PWD="+pw)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		_ = os.Remove(file)
		return fmt.Errorf("backup failed: %w: %s", err, tail(out.String(), 400))
	}
	return nil
}

// ResetDatabase drops and recreates a database, leaving it empty.
func (r *Runtime) ResetDatabase(ctx context.Context, db string) error {
	pw := secrets.RootPassword()
	if _, err := r.mysql(ctx, pw, "", "DROP DATABASE IF EXISTS `"+db+"`"); err != nil {
		return err
	}
	_, err := r.mysql(ctx, pw, "", "CREATE DATABASE `"+db+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
	return err
}

// mysql runs one statement. extra is an optional single flag.
func (r *Runtime) mysql(ctx context.Context, pw, extra, sql string) (string, error) {
	// A server that accepts the connection and then stalls must not hang the caller forever.
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	args := []string{"--defaults-file=" + r.myCnf(), "--connect-timeout=10", "-uroot", "-N", "-B"}
	if extra != "" {
		args = append(args, extra)
	}
	args = append(args, "-e", sql)
	cmd := exec.CommandContext(ctx, r.mysqlBin("mysql"), args...)
	if pw != "" {
		cmd.Env = append(os.Environ(), "MYSQL_PWD="+pw)
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return out.String(), fmt.Errorf("MySQL did not answer within 45 seconds (see the mysql logs in the logs folder)")
		}
		return out.String(), fmt.Errorf("mysql: %w: %s", err, strings.TrimSpace(tail(out.String(), 300)))
	}
	return out.String(), nil
}

// StopMySQL shuts MySQL down cleanly if possible.
func (r *Runtime) StopMySQL(ctx context.Context) error {
	if process.Running(r.mysqlSpec()) {
		cmd := exec.CommandContext(ctx, r.mysqlBin("mysqladmin"),
			"--defaults-file="+r.myCnf(), "-uroot", "shutdown")
		if pw := secrets.RootPassword(); pw != "" {
			cmd.Env = append(os.Environ(), "MYSQL_PWD="+pw)
		}
		_ = cmd.Run()
	}
	return process.Stop(r.mysqlSpec())
}

func execCombined(bin, dir string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func tail(s string, n int) string {
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}
