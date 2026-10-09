package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// pickFolder shows the OS folder chooser and returns the selection ("" if cancelled).
func pickFolder(ctx context.Context) (string, error) {
	if runtime.GOOS != "windows" {
		return osascript(ctx, `POSIX path of (choose folder with prompt "Choose a project folder")`)
	}
	return powershellDialog(ctx, `
Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = 'Choose a project folder (it contains hyper.json)'
$d.ShowNewFolderButton = $false
$f = New-Object System.Windows.Forms.Form -Property @{ TopMost = $true; ShowInTaskbar = $false; Opacity = 0 }
$f.Show()
if ($d.ShowDialog($f) -eq 'OK') { [Console]::Out.Write($d.SelectedPath) }
$f.Close()`)
}

// pickFile shows the OS file chooser for SQL dumps.
func pickFile(ctx context.Context) (string, error) {
	if runtime.GOOS != "windows" {
		return osascript(ctx, `POSIX path of (choose file with prompt "Choose a SQL dump")`)
	}
	return powershellDialog(ctx, `
Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Filter = 'SQL dumps (*.sql;*.sql.gz;*.gz)|*.sql;*.sql.gz;*.gz|All files (*.*)|*.*'
$d.Title = 'Choose a SQL dump'
$f = New-Object System.Windows.Forms.Form -Property @{ TopMost = $true; ShowInTaskbar = $false; Opacity = 0 }
$f.Show()
if ($d.ShowDialog($f) -eq 'OK') { [Console]::Out.Write($d.FileName) }
$f.Close()`)
}

func powershellDialog(ctx context.Context, script string) (string, error) {
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-STA", "-WindowStyle", "Hidden", "-Command", script).Output()
	if err != nil {
		return "", fmt.Errorf("cannot open the file chooser: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func osascript(ctx context.Context, expr string) (string, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", expr).Output()
	if err != nil {
		return "", nil // cancelled
	}
	return strings.TrimSpace(string(out)), nil
}
