// Package elevate runs the few operations that need Administrator rights
// (trusting the local CA, letting the user edit the hosts file) in a short-lived
// elevated copy of hyper, so the tool itself never runs as admin.
package elevate

import (
	"crypto/sha1" //nolint:gosec // see Thumbprint
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// CAName is the CA's common name, used to find it in the trust store.
const CAName = "Hyper Stack Local CA"

// HostsPath is the system hosts file.
func HostsPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("SystemRoot"), "System32", "drivers", "etc", "hosts")
	}
	return "/etc/hosts"
}

// HostsWritable reports whether the current user can modify the hosts file.
func HostsWritable() bool {
	f, err := os.OpenFile(HostsPath(), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// Thumbprint returns the SHA-1 fingerprint of the first certificate in a PEM
// file, the same identifier Windows uses to find it in the trust store.
func Thumbprint(pemPath string) (string, error) {
	b, err := os.ReadFile(pemPath)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return "", fmt.Errorf("%s is not a PEM certificate", pemPath)
	}
	sum := sha1.Sum(block.Bytes) //nolint:gosec // SHA-1 is Windows' certificate identifier here, not a security check
	return hex.EncodeToString(sum[:]), nil
}

// CATrusted reports whether THIS CA certificate (not just one with the same
// name) is in the machine trust store. A new install folder creates a new CA,
// which must be trusted again.
func CATrusted(caPath string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	tp, err := Thumbprint(caPath)
	if err != nil {
		return false
	}
	return exec.Command("certutil", "-verifystore", "Root", tp).Run() == nil
}

// CATrustedByName reports whether any CA with our name is trusted (used by uninstall).
func CATrustedByName() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	return exec.Command("certutil", "-store", "Root", CAName).Run() == nil
}

// SetupNeeded reports whether hyper install must request elevation for this CA.
func SetupNeeded(caPath string) bool { return !HostsWritable() || !CATrusted(caPath) }

// RunSetup relaunches this executable elevated (one UAC prompt) to run Do("setup", ...).
func RunSetup(caPath string) error {
	if runtime.GOOS != "windows" {
		return fmt.Errorf("elevation is only implemented for Windows in this build")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	user := os.Getenv("USERDOMAIN") + `\` + os.Getenv("USERNAME")
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	argStr := fmt.Sprintf(`_elevated setup "%s" "%s"`, caPath, user)
	script := fmt.Sprintf("$p = Start-Process -FilePath %s -ArgumentList %s -Verb RunAs -Wait -PassThru; exit $p.ExitCode", q(exe), q(argStr))
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput() //nolint:gosec // arguments are this exe and the CA path, quoted for PowerShell
	if err != nil {
		return fmt.Errorf("the elevated setup did not complete (was the UAC prompt declined?): %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Do performs an elevated operation. Called from the hidden _elevated command.
func Do(op string, args []string) error {
	switch op {
	case "setup": // setup <caPath> <user>
		if len(args) != 2 {
			return fmt.Errorf("setup needs <caPath> <user>")
		}
		if out, err := exec.Command("certutil", "-addstore", "-f", "Root", args[0]).CombinedOutput(); err != nil {
			return fmt.Errorf("certutil failed: %w\n%s", err, out)
		}
		if out, err := exec.Command("icacls", HostsPath(), "/grant", args[1]+":(M)").CombinedOutput(); err != nil {
			return fmt.Errorf("icacls failed: %w\n%s", err, out)
		}
		return nil
	case "teardown": // teardown <user>
		_ = exec.Command("certutil", "-delstore", "Root", CAName).Run()
		if len(args) == 1 {
			_ = exec.Command("icacls", HostsPath(), "/remove:g", args[0]).Run()
		}
		return nil
	}
	return fmt.Errorf("unknown elevated operation %q", op)
}

// RunTeardown removes the CA and the hosts permission (one UAC prompt).
func RunTeardown() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	user := os.Getenv("USERDOMAIN") + `\` + os.Getenv("USERNAME")
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	argStr := fmt.Sprintf(`_elevated teardown "%s"`, user)
	script := fmt.Sprintf("$p = Start-Process -FilePath %s -ArgumentList %s -Verb RunAs -Wait -PassThru; exit $p.ExitCode", q(exe), q(argStr))
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput() //nolint:gosec // arguments are this exe and the current user name, quoted for PowerShell
	if err != nil {
		return fmt.Errorf("the elevated teardown did not complete: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
