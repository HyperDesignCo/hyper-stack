package elevate

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

const systemKeychain = "/Library/Keychains/System.keychain"

// macSHA1s lists the SHA-1 fingerprints of the certificates in the system keychain
// whose name is CAName (upper case, as "security" prints them).
func macSHA1s() []string {
	out, err := exec.Command("security", "find-certificate", "-a", "-Z", "-c", CAName, systemKeychain).Output()
	if err != nil {
		return nil
	}
	return parseSecurityHashes(string(out))
}

// parseSecurityHashes extracts the "SHA-1 hash: XXXX" lines of "security find-certificate -Z".
func parseSecurityHashes(out string) []string {
	var hashes []string
	for _, l := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "SHA-1 hash:"); ok {
			hashes = append(hashes, strings.ToUpper(strings.TrimSpace(rest)))
		}
	}
	return hashes
}

func macTrusted(caPath string) bool {
	tp, err := Thumbprint(caPath)
	if err != nil {
		return false
	}
	for _, h := range macSHA1s() {
		if strings.EqualFold(h, tp) {
			return true
		}
	}
	return false
}

// macSetupScript trusts the CA in the system keychain and lets the current user
// edit /etc/hosts (the same two things the Windows setup does).
func macSetupScript(caPath, username string) string {
	return fmt.Sprintf(`/usr/bin/security add-trusted-cert -d -r trustRoot -k %s %s && /usr/sbin/chown %s /etc/hosts && /bin/chmod 644 /etc/hosts`,
		systemKeychain, shQuote(caPath), shQuote(username))
}

// macTeardownScript removes every certificate named CAName and gives /etc/hosts back to root.
func macTeardownScript() string {
	return fmt.Sprintf(`n=0; while [ $n -lt 20 ] && /usr/bin/security delete-certificate -t -c %s %s >/dev/null 2>&1; do n=$((n+1)); done; /usr/sbin/chown root:wheel /etc/hosts; /bin/chmod 644 /etc/hosts`,
		shQuote(CAName), systemKeychain)
}

// shQuote quotes a string for /bin/sh.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// appleScriptQuote quotes a string for an AppleScript literal.
func appleScriptQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// runPrivileged runs a shell script as root. It tries sudo without a password
// first (a machine where that works, such as CI, needs no prompt), and otherwise
// shows the standard macOS password dialog once.
func runPrivileged(script, why string) error {
	if exec.Command("sudo", "-n", "true").Run() == nil {
		if out, err := exec.Command("sudo", "-n", "/bin/sh", "-c", script).CombinedOutput(); err != nil {
			return fmt.Errorf("the privileged step failed: %w\n%s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	osa := fmt.Sprintf(`do shell script %s with administrator privileges with prompt %s`, appleScriptQuote(script), appleScriptQuote(why))
	out, err := exec.Command("osascript", "-e", osa).CombinedOutput()
	if err != nil {
		return fmt.Errorf("the macOS password prompt did not complete (was it cancelled?): %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func macRunSetup(caPath string) error {
	username := os.Getenv("USER")
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	if username == "" {
		return fmt.Errorf("cannot tell which user to give access to /etc/hosts")
	}
	return runPrivileged(macSetupScript(caPath, username),
		"Hyper Stack needs your password once, to trust its local certificate and to let it edit /etc/hosts.")
}

func macRunTeardown() error {
	return runPrivileged(macTeardownScript(),
		"Hyper Stack needs your password to remove its certificate and give /etc/hosts back to the system.")
}
