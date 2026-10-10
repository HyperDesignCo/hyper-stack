package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// psPrelude is shared by the Windows choosers. A background program cannot take
// the focus on Windows, so without this the dialog opens BEHIND the browser and
// looks like nothing happened. A synthetic Alt tap lifts that restriction for
// the next SetForegroundWindow call, and the invisible owner window is topmost
// so the dialog is too.
const psPrelude = `
Add-Type -AssemblyName System.Windows.Forms
Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class HyperFg {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr hWnd);
  [DllImport("user32.dll")] public static extern void keybd_event(byte vk, byte scan, uint flags, UIntPtr extra);
}
'@
function New-Owner {
  $f = New-Object System.Windows.Forms.Form -Property @{ TopMost = $true; ShowInTaskbar = $false; Opacity = 0; StartPosition = 'CenterScreen'; Width = 1; Height = 1 }
  $f.Show()
  [HyperFg]::keybd_event(0x12, 0, 0, [UIntPtr]::Zero)
  [HyperFg]::keybd_event(0x12, 0, 2, [UIntPtr]::Zero)
  [void][HyperFg]::SetForegroundWindow($f.Handle)
  $f.Activate()
  return $f
}
`

// pickFolder shows the OS folder chooser and returns the selection ("" if cancelled).
func pickFolder(ctx context.Context) (string, error) {
	if runtime.GOOS != "windows" {
		return osascript(ctx, `POSIX path of (choose folder with prompt "Choose a project folder")`)
	}
	return powershellDialog(ctx, psPrelude+`
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = 'Choose a folder'
$d.ShowNewFolderButton = $true
$f = New-Owner
if ($d.ShowDialog($f) -eq 'OK') { [Console]::Out.Write($d.SelectedPath) }
$f.Close()`)
}

// pickFile shows the OS file chooser for SQL dumps.
func pickFile(ctx context.Context) (string, error) {
	if runtime.GOOS != "windows" {
		return osascript(ctx, `POSIX path of (choose file with prompt "Choose a SQL dump")`)
	}
	return powershellDialog(ctx, psPrelude+`
$d = New-Object System.Windows.Forms.OpenFileDialog
$d.Filter = 'SQL dumps (*.sql;*.sql.gz;*.gz)|*.sql;*.sql.gz;*.gz|All files (*.*)|*.*'
$d.Title = 'Choose a SQL dump'
$f = New-Owner
if ($d.ShowDialog($f) -eq 'OK') { [Console]::Out.Write($d.FileName) }
$f.Close()`)
}

// chooserOpen allows one chooser at a time. Clicking again while a window is
// still open used to stack more of them.
var chooserOpen atomic.Bool

func powershellDialog(ctx context.Context, script string) (string, error) {
	if !chooserOpen.CompareAndSwap(false, true) {
		return "", fmt.Errorf("a file chooser is already open. Look for it on the taskbar, or close it, before opening another")
	}
	defer chooserOpen.Store(false)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	// A script file, not -Command: a long multi-line script passed as an argument
	// is mangled by command-line quoting, and its errors vanish.
	f, err := os.CreateTemp("", "hyper-chooser-*.ps1")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	// Windows PowerShell 5.1 reads a script as ANSI unless it starts with a BOM.
	if _, err := f.Write(append([]byte{0xEF, 0xBB, 0xBF}, script...)); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-STA", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-File", f.Name()) //nolint:gosec // fixed PowerShell script, no user input
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("cannot open the file chooser: %w %s", err, strings.TrimSpace(stderr.String()))
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" && strings.TrimSpace(string(out)) == "" {
		return "", fmt.Errorf("the file chooser failed: %s", msg)
	}
	return strings.TrimSpace(string(out)), nil
}

func osascript(ctx context.Context, expr string) (string, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", expr).Output() //nolint:gosec // fixed AppleScript expression, no user input
	if err != nil {
		return "", nil // cancelled
	}
	return strings.TrimSpace(string(out)), nil
}
