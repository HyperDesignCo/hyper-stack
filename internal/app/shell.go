package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hyperdesign/hyper-stack/internal/process"
)

// shellCommand runs a command line the way the platform's shell does: cmd on
// Windows, sh on macOS.
func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "/bin/sh", []string{"-c", command}
}

// composerShim writes a small "composer" command that runs composer.phar with the
// project's PHP, and returns the folder it is in (to be put on PATH).
func (a *App) composerShim(php string) string {
	dir := filepath.Join(a.P.Run, "shims", php)
	_ = os.MkdirAll(dir, 0o755)
	if runtime.GOOS == "windows" {
		shim := "@echo off\r\n\"" + a.RT.PHPExe(php) + "\" \"" + a.RT.ComposerPhar() + "\" %*\r\n"
		_ = os.WriteFile(filepath.Join(dir, "composer.bat"), []byte(shim), 0o644) //nolint:gosec // our own data directory
		return dir
	}
	shim := "#!/bin/sh\nexec \"" + a.RT.PHPExe(php) + "\" \"" + a.RT.ComposerPhar() + "\" \"$@\"\n"
	_ = os.WriteFile(filepath.Join(dir, "composer"), []byte(shim), 0o755) //nolint:gosec // an executable script in our own data directory
	return dir
}

// shellEnv is the environment for a project's setup commands and workers: its
// PHP, Node and Composer first on PATH.
func (a *App) shellEnv(s Site) []string {
	path := strings.Join([]string{filepath.Dir(a.RT.PHPExe(s.PHP)), a.RT.NodeBinDir(), a.composerShim(s.PHP), os.Getenv("PATH")}, string(os.PathListSeparator))
	return append(os.Environ(), "PATH="+path,
		"PHPRC="+filepath.Dir(a.RT.PHPExe(s.PHP)), // a static PHP finds its php.ini this way
		"COMPOSER_HOME="+filepath.Join(a.P.Root, "composer-home"))
}

func (a *App) runShell(ctx context.Context, s Site, command string) error {
	name, args := shellCommand(command)
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // the project's own setup commands, shown to the user for review
	cmd.Dir = s.Root
	cmd.Env = a.shellEnv(s)
	cmd.Stdout, cmd.Stderr = a.Out, a.Out
	return cmd.Run()
}

func (a *App) workerSpec(s Site, i int, command string) process.Spec {
	name, args := shellCommand(command)
	return process.Spec{
		Name: workerName(s.Name, i), Bin: name, Args: args, Dir: s.Root,
		Env: envDelta(a.shellEnv(s)), RunDir: a.P.Run, LogFile: filepath.Join(a.P.Logs, fmt.Sprintf("%s-worker-%d.log", s.Name, i)),
	}
}

// envDelta keeps only what we add to the environment (process.Start appends it to os.Environ).
func envDelta(env []string) []string {
	var out []string
	for _, e := range env {
		u := strings.ToUpper(e)
		if strings.HasPrefix(u, "PATH=") || strings.HasPrefix(u, "PHPRC=") || strings.HasPrefix(e, "COMPOSER_HOME=") {
			out = append(out, e)
		}
	}
	return out
}
