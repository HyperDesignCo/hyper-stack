// Package doctor inspects the machine for things that stop Hyper Stack working.
package doctor

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// Owner is a process listening on a TCP port.
type Owner struct {
	PID  int
	Name string
	Path string
}

// PortOwner returns who listens on port, if anyone.
func PortOwner(port int) (Owner, bool) {
	if runtime.GOOS != "windows" {
		return unixPortOwner(port)
	}
	return windowsPortOwner(port)
}

// windowsPortOwner asks netstat and the Windows process API. (It used to run
// PowerShell's Get-NetTCPConnection, which can stall for a long time and then
// kept the whole Start button waiting.)
func windowsPortOwner(port int) (Owner, bool) {
	out, err := netstatListening()
	if err != nil {
		return Owner{}, false
	}
	pid := listenPID(out, port)
	if pid == 0 {
		return Owner{}, false
	}
	name, path := processInfo(pid)
	return Owner{PID: pid, Name: name, Path: path}, true
}

// listenPID finds the process listening on port in "netstat -ano -p TCP" output.
func listenPID(out string, port int) int {
	suffix := ":" + strconv.Itoa(port)
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 5 || !strings.EqualFold(f[0], "TCP") || !strings.EqualFold(f[3], "LISTENING") || !strings.HasSuffix(f[1], suffix) {
			continue
		}
		if pid, err := strconv.Atoi(f[4]); err == nil {
			return pid
		}
	}
	return 0
}

func parseOwner(s string) (Owner, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Owner{}, false
	}
	f := strings.SplitN(s, "|", 3)
	pid, err := strconv.Atoi(f[0])
	if err != nil {
		return Owner{}, false
	}
	o := Owner{PID: pid}
	if len(f) > 1 {
		o.Name = f[1]
	}
	if len(f) > 2 {
		o.Path = f[2]
	}
	return o, true
}

func unixPortOwner(port int) (Owner, bool) {
	out, err := exec.Command("lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-Fpc").Output()
	if err != nil {
		return Owner{}, false
	}
	var o Owner
	for _, l := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(l, "p"):
			o.PID, _ = strconv.Atoi(l[1:])
		case strings.HasPrefix(l, "c"):
			o.Name = l[1:]
		}
	}
	if o.PID != 0 {
		// On macOS ps reports the full path of the program in comm.
		if p, err := exec.Command("ps", "-p", strconv.Itoa(o.PID), "-o", "comm=").Output(); err == nil {
			o.Path = strings.TrimSpace(string(p))
		}
	}
	return o, o.PID != 0
}

// Hint suggests a fix for a conflicting listener.
func Hint(o Owner, port int) string {
	lower := strings.ToLower(o.Path + " " + o.Name)
	switch {
	case strings.Contains(lower, "laragon"):
		return fmt.Sprintf("Laragon is using port %d. Stop its service in Laragon, or run: hyper config set <http_port|https_port|mysql_port> <other port>", port)
	case strings.Contains(lower, "xampp"):
		return fmt.Sprintf("XAMPP is using port %d. Stop it in the XAMPP control panel, or choose another port with hyper config set", port)
	case strings.Contains(lower, "w3wp") || strings.Contains(lower, "inetinfo") || strings.Contains(lower, "iis"):
		return fmt.Sprintf("IIS is using port %d. Stop it with iisreset /stop (as admin), or choose another port with hyper config set", port)
	case strings.Contains(lower, "mysql"):
		return fmt.Sprintf("Another MySQL is using port %d. Stop that service, or run: hyper config set mysql_port <other port>", port)
	}
	return fmt.Sprintf("Stop %s (pid %d), or choose another port with: hyper config set <http_port|https_port|mysql_port> <port>", o.Name, o.PID)
}
