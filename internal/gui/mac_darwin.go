//go:build darwin && cgo

package gui

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
#include "cocoa_darwin.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/hyperdesign/hyper-stack/internal/app"
	"github.com/hyperdesign/hyper-stack/internal/brand"
	"github.com/hyperdesign/hyper-stack/internal/config"
)

func init() {
	// AppKit must run on the main thread, which is where the main goroutine starts.
	runtime.LockOSThread()
}

// Available reports whether the native window exists on this platform.
func Available() bool { return true }

// Show brings a running window to the front. Another process cannot do that
// here, so a second "hyper ui" is handled by the caller opening the browser.
func Show() bool { return false }

type macWindow struct {
	b        Backend
	onQuit   func()
	mu       sync.Mutex
	state    app.State
	have     bool
	selected string
	lastJob  string
	lastLog  string
	lastSt   string
}

var theWindow *macWindow

//export goAction
func goAction(name, arg *C.char) {
	n, a := C.GoString(name), C.GoString(arg)
	go theWindow.handle(n, a) // never block the UI thread
}

// Run shows the window (unless show is false) and the menu bar icon, and
// returns when the user chooses Quit. onQuit runs just before it returns.
func Run(b Backend, show bool, onQuit func()) error {
	w := &macWindow{b: b, onQuit: onQuit}
	theWindow = w
	go func() {
		time.Sleep(300 * time.Millisecond) // let the run loop start
		icon := brand.PNG(256)
		C.hs_set_icon(unsafe.Pointer(&icon[0]), C.int(len(icon)))
		w.refresh()
		w.pollJobs()
	}()
	go func() {
		for {
			time.Sleep(4 * time.Second)
			w.refresh()
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGUSR1) // a second "hyper ui" asks this window to come forward
	go func() {
		for range sig {
			C.hs_show()
		}
	}()
	flag := 0
	if show {
		flag = 1
	}
	C.hs_run(C.int(flag))
	if w.onQuit != nil {
		w.onQuit()
	}
	return nil
}

// ---------------------------------------------------------------- state -> UI

func siteStatus(s app.SiteInfo) string {
	switch {
	case s.Error != "":
		return "Problem"
	case s.Online:
		return "Running"
	case s.Discovered:
		return "Not started yet"
	case !s.RootExists:
		return "Folder missing"
	}
	return "Stopped"
}

func yes(b bool) string {
	if b {
		return "Yes"
	}
	return ""
}

func cstr(s string) *C.char { return C.CString(s) }

func (w *macWindow) refresh() {
	a, err := w.b.NewApp(io.Discard)
	if err != nil {
		return
	}
	st := a.State(context.Background())
	a.Close()
	w.mu.Lock()
	w.state, w.have = st, true
	w.mu.Unlock()

	type site struct {
		Name  string   `json:"name"`
		Cells []string `json:"cells"`
	}
	online := 0
	var sites []site
	for _, s := range st.Sites {
		if s.Online {
			online++
		}
		sites = append(sites, site{s.Name, []string{s.Name, strings.TrimPrefix(strings.TrimPrefix(s.URL, "https://"), "http://"), s.PHP, siteStatus(s), yes(s.Autostart)}})
	}
	var services [][]string
	for _, s := range st.Services {
		state, port, pid := "Stopped", "", ""
		if s.Running {
			state = "Running"
		}
		if s.Port > 0 {
			port = fmt.Sprint(s.Port)
		}
		if s.PID > 0 {
			pid = fmt.Sprint(s.PID)
		}
		services = append(services, []string{s.Name, state, port, pid})
	}
	status := "Hyper Stack is not installed yet. Press Install."
	switch {
	case !st.Configured || !st.Installed:
	case st.AnyRunning:
		status = fmt.Sprintf("Running: %d project(s) online. Projects live in %s", online, st.WWW)
	default:
		status = fmt.Sprintf("Everything is stopped. Projects live in %s", st.WWW)
	}
	set := fmt.Sprintf("Install folder: %s\nProjects folder (www): %s\nWeb ports: http %d, https %d\nMySQL: 127.0.0.1:%d, user root, no password\nphpMyAdmin: %s\nHosts file writable: %v\n",
		st.DataDir, st.WWW, st.Settings.HTTPPort, st.Settings.HTTPSPort, st.Settings.MySQLPort, st.PMAURL, st.HostsWrite)
	out, _ := json.Marshal(map[string]any{
		"status": status, "installVisible": !st.Installed, "allEnabled": st.Installed, "loginOn": st.LoginStart,
		"settings": set, "php": st.PHPVersions, "logs": st.LogFiles, "sites": sites, "services": services,
	})
	cs := cstr(string(out))
	C.hs_update(cs)
	C.free(unsafe.Pointer(cs))
}

func (w *macWindow) current() (app.SiteInfo, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, s := range w.state.Sites {
		if s.Name == w.selected {
			return s, true
		}
	}
	return app.SiteInfo{}, false
}

func (w *macWindow) busy() bool {
	_, st, _, _ := w.b.Jobs.Snapshot()
	return st == "running"
}

func (w *macWindow) pushSelection() {
	s, ok := w.current()
	w.mu.Lock()
	installed := w.state.Installed
	versions := w.state.PHPVersions
	w.mu.Unlock()
	busy := w.busy()
	idx := -1
	for i, v := range versions {
		if v == s.PHP {
			idx = i
		}
	}
	out, _ := json.Marshal(map[string]any{
		"start": ok && installed && !busy, "stop": ok && s.Online && !busy, "open": ok && s.Online, "pma": ok && installed,
		"import": ok && installed, "folder": ok, "switchphp": ok && installed && !busy, "forget": ok && !s.Discovered && !busy,
		"siteauto": ok && installed, "siteautoOn": ok && s.Autostart, "phpIndex": idx,
	})
	cs := cstr(string(out))
	C.hs_set_selection(cs)
	C.free(unsafe.Pointer(cs))
}

func setJob(title, log string) {
	t, l := cstr(title), cstr(log)
	C.hs_set_job(t, l)
	C.free(unsafe.Pointer(t))
	C.free(unsafe.Pointer(l))
}

func setText(which int, text string) {
	t := cstr(text)
	C.hs_set_text(C.int(which), t)
	C.free(unsafe.Pointer(t))
}

func (w *macWindow) pollJobs() {
	for {
		time.Sleep(500 * time.Millisecond)
		title, st, errText, log := w.b.Jobs.Snapshot()
		label := ""
		switch st {
		case "running":
			label = title + "  (working...)"
		case "done":
			label = title + "  (done)"
		case "failed":
			label = title + "  (failed): " + firstLine(errText)
		}
		if label != w.lastJob || log != w.lastLog {
			w.lastJob, w.lastLog = label, log
			setJob(label, log)
		}
		if st != w.lastSt {
			prev := w.lastSt
			w.lastSt = st
			w.pushSelection()
			if prev == "running" {
				w.refresh()
				if st == "failed" {
					alert(title, "That did not work:\n\n"+errText, true)
				}
			}
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// ---------------------------------------------------------------- dialogs

func alert(title, msg string, warn bool, buttons ...string) int {
	if len(buttons) == 0 {
		buttons = []string{"OK"}
	}
	var b [3]*C.char
	for i := range b {
		if i < len(buttons) {
			b[i] = cstr(buttons[i])
		}
	}
	t, m := cstr(title), cstr(msg)
	w := 0
	if warn {
		w = 1
	}
	r := int(C.hs_alert(t, m, C.int(w), b[0], b[1], b[2]))
	C.free(unsafe.Pointer(t))
	C.free(unsafe.Pointer(m))
	for _, p := range b {
		if p != nil {
			C.free(unsafe.Pointer(p))
		}
	}
	return r
}

func chooseFile(title string) string {
	t := cstr(title)
	defer C.free(unsafe.Pointer(t))
	p := C.hs_open_file(t)
	if p == nil {
		return ""
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p)
}

// ---------------------------------------------------------------- actions

func (w *macWindow) run(title string, fn func(ctx context.Context, a *app.App) error) {
	err := w.b.Jobs.Start(title, func(ctx context.Context, out io.Writer) error {
		a, err := w.b.NewApp(out)
		if err != nil {
			return err
		}
		defer a.Close()
		return fn(ctx, a)
	})
	if err != nil {
		alert("Hyper Stack", err.Error(), false)
	}
}

func (w *macWindow) handle(name, arg string) {
	switch name {
	case "select":
		w.mu.Lock()
		w.selected = arg
		w.mu.Unlock()
		w.pushSelection()
	case "show":
		C.hs_show()
	case "web":
		if w.b.WebURL != nil && w.b.OpenURL != nil {
			_ = w.b.OpenURL(w.b.WebURL())
		}
	case "quit":
		C.hs_quit()
	case "startall":
		w.run("Start all projects", func(ctx context.Context, a *app.App) error { return a.UpAll(ctx) })
	case "stopall":
		w.run("Stop all services", func(ctx context.Context, a *app.App) error { return a.Down(ctx, "", true) })
	case "www":
		w.run("Open www folder", func(ctx context.Context, a *app.App) error { return a.OpenWWW() })
	case "install":
		w.install()
	case "start":
		w.start()
	case "stop":
		if s, ok := w.current(); ok {
			w.run("Stop "+s.Name, func(ctx context.Context, a *app.App) error { return a.StopSite(ctx, s.Name) })
		}
	case "open":
		if s, ok := w.current(); ok && s.URL != "" && w.b.OpenURL != nil {
			_ = w.b.OpenURL(s.URL)
		}
	case "pma":
		if s, ok := w.current(); ok {
			w.run("Open phpMyAdmin for "+s.Name, func(ctx context.Context, a *app.App) error { return a.DBOpen(ctx, s.Root) })
		}
	case "folder":
		if s, ok := w.current(); ok && w.b.OpenURL != nil {
			_ = w.b.OpenURL(s.Root)
		}
	case "import":
		w.importSQL()
	case "switchphp":
		if s, ok := w.current(); ok && arg != "" && arg != s.PHP {
			w.run("Switch "+s.Name+" to PHP "+arg, func(ctx context.Context, a *app.App) error { return a.SetProjectPHP(ctx, s.Root, arg) })
		}
	case "siteauto":
		if s, ok := w.current(); ok {
			a, err := w.b.NewApp(io.Discard)
			if err == nil {
				err = a.SetProjectAutostart(s.Root, arg == "1")
				a.Close()
			}
			if err != nil {
				alert("Hyper Stack", err.Error(), true)
			}
			w.refresh()
		}
	case "login":
		a, err := w.b.NewApp(io.Discard)
		if err == nil {
			err = a.SetLoginAutostart(arg == "1")
			a.Close()
		}
		if err != nil {
			alert("Hyper Stack", err.Error(), true)
		}
		w.refresh()
	case "forget":
		if s, ok := w.current(); ok {
			if alert("Remove "+s.Name, fmt.Sprintf("Remove %s from Hyper Stack?\n\nIts files and database are not deleted.", s.Name), false, "Remove", "Cancel") == 0 {
				w.run("Remove "+s.Name, func(ctx context.Context, a *app.App) error { return a.ForgetSite(ctx, s.Name) })
			}
		}
	case "check":
		w.check()
	case "fix":
		w.run("Apply safe fixes", func(ctx context.Context, a *app.App) error { return a.Doctor(ctx, true) })
	case "logfile":
		w.loadLog(arg)
	case "logrefresh":
		if arg != "" {
			w.loadLog(arg)
		}
	}
}

func (w *macWindow) start() {
	s, ok := w.current()
	if !ok {
		return
	}
	a, err := w.b.NewApp(io.Discard)
	if err != nil {
		alert("Hyper Stack", err.Error(), true)
		return
	}
	ins, err := a.Inspect(s.Root)
	a.Close()
	if err != nil {
		alert("Hyper Stack", err.Error(), true)
		return
	}
	if ins.NeedsConfirm {
		msg := "This project wants to run these commands the first time it starts:\n\n  " + strings.Join(ins.Setup, "\n  ") +
			"\n\nOnly continue if you trust this project."
		if alert("Start "+ins.Name, msg, true, "Run them", "Cancel") != 0 {
			return
		}
	}
	w.run("Start "+ins.Name, func(ctx context.Context, a *app.App) error { return a.Up(ctx, ins.Root) })
}

func (w *macWindow) importSQL() {
	s, ok := w.current()
	if !ok {
		return
	}
	file := chooseFile("Choose a SQL dump for " + s.Name)
	if file == "" {
		return
	}
	low := strings.ToLower(file)
	if !strings.HasSuffix(low, ".sql") && !strings.HasSuffix(low, ".sql.gz") && !strings.HasSuffix(low, ".gz") {
		alert("Import", "Choose a .sql or .sql.gz file.", true)
		return
	}
	r := alert("Import "+filepath.Base(file), "Replace the database?\n\nReplace: empty "+s.Database+" first (a backup is saved), then import.\nAdd: put the dump on top of what is there.",
		false, "Replace", "Add", "Cancel")
	if r == 2 {
		return
	}
	w.run("Import "+filepath.Base(file)+" into "+s.Name, func(ctx context.Context, a *app.App) error {
		return a.DBImport(ctx, s.Root, file, r == 0)
	})
}

func (w *macWindow) check() {
	setText(0, "Checking...")
	a, err := w.b.NewApp(io.Discard)
	if err != nil {
		setText(0, err.Error())
		return
	}
	fs := a.Checks(context.Background())
	a.Close()
	var sb strings.Builder
	for _, f := range fs {
		mark := "[ ok ]"
		if !f.OK {
			mark = "[FAIL]"
		}
		fmt.Fprintf(&sb, "%s %s\n", mark, f.Title)
		if !f.OK && f.Detail != "" {
			fmt.Fprintf(&sb, "         %s\n", f.Detail)
		}
		if !f.OK && f.Fix != "" {
			fmt.Fprintf(&sb, "         Fix: %s\n", f.Fix)
		}
	}
	setText(0, sb.String())
}

func (w *macWindow) loadLog(name string) {
	a, err := w.b.NewApp(io.Discard)
	if err != nil {
		return
	}
	txt, err := a.TailLog(name, 300)
	a.Close()
	if err != nil {
		txt = err.Error()
	}
	setText(1, txt)
}

func (w *macWindow) install() {
	w.mu.Lock()
	st, have := w.state, w.have
	w.mu.Unlock()
	if !have {
		return
	}
	dir := st.DataDir
	if !st.Configured {
		dir = st.DefaultDir
	}
	editable := 1
	if st.Configured {
		editable = 0
	}
	hp, sp, mp := st.Settings.HTTPPort, st.Settings.HTTPSPort, st.Settings.MySQLPort
	if !st.Installed {
		hp, sp, mp, _ = suggestPorts(hp, sp, mp)
	}
	d := cstr(dir)
	defer C.free(unsafe.Pointer(d))
	var outDir *C.char
	var ports [3]C.int
	ok := C.hs_install_dialog(d, C.int(editable), C.int(hp), C.int(sp), C.int(mp), &outDir, &ports[0])
	if ok == 0 {
		return
	}
	chosen := strings.TrimSpace(C.GoString(outDir))
	C.free(unsafe.Pointer(outDir))
	if !st.Configured {
		abs, err := config.ValidateInstallDir(chosen)
		if err != nil {
			alert("Install Hyper Stack", err.Error(), true)
			return
		}
		chosen = abs
	}
	p := app.Ports{HTTP: int(ports[0]), HTTPS: int(ports[1]), MySQL: int(ports[2])}
	w.run("Install Hyper Stack", func(ctx context.Context, a *app.App) error { return a.InstallTo(ctx, chosen, p) })
}
