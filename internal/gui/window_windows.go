//go:build windows

package gui

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/tailscale/walk"
	. "github.com/tailscale/walk/declarative" //nolint:staticcheck // the declarative UI reads best dot-imported
	"github.com/tailscale/win"
	"golang.org/x/sys/windows"

	"github.com/hyperdesign/hyper-stack/internal/app"
	"github.com/hyperdesign/hyper-stack/internal/config"
	"github.com/hyperdesign/hyper-stack/internal/tray"
)

// Available reports whether the native window exists on this platform.
func Available() bool { return true }

// instanceSuffix keeps separate Hyper Stack homes (HYPER_HOME, used for testing) apart.
func instanceSuffix() string {
	h := os.Getenv("HYPER_HOME")
	if h == "" {
		return ""
	}
	sum := fnv.New32a()
	_, _ = sum.Write([]byte(strings.ToLower(h)))
	return fmt.Sprintf("-%08x", sum.Sum32())
}

func showEventName() string { return `Local\HyperStackShowWindow` + instanceSuffix() }

var instanceMutex windows.Handle

// Acquire claims the right to be the one running window. It returns false when
// another Hyper Stack window process already exists, in which case the caller
// should call Show and exit instead of opening a second window.
func Acquire() bool {
	name, err := windows.UTF16PtrFromString(`Local\HyperStackWindowInstance` + instanceSuffix())
	if err != nil {
		return true
	}
	h, err := windows.CreateMutex(nil, false, name)
	if h == 0 {
		return true // cannot tell: do not block the user
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(h)
		return false
	}
	instanceMutex = h // held until this process exits
	runtime.KeepAlive(instanceMutex)
	return true
}

// Show asks the running window to come to the front. It reports whether one was running.
func Show() bool {
	name, err := windows.UTF16PtrFromString(showEventName())
	if err != nil {
		return false
	}
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()
	return windows.SetEvent(h) == nil
}

// ---------------------------------------------------------------- table models

type siteModel struct {
	walk.TableModelBase
	rows []app.SiteInfo
}

func (m *siteModel) RowCount() int { return len(m.rows) }

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

func (m *siteModel) Value(row, col int) interface{} {
	s := m.rows[row]
	switch col {
	case 0:
		return s.Name
	case 1:
		return strings.TrimPrefix(strings.TrimPrefix(s.URL, "https://"), "http://")
	case 2:
		return s.PHP
	case 3:
		return siteStatus(s)
	case 4:
		if s.Autostart {
			return "Yes"
		}
		return ""
	}
	return ""
}

type svcModel struct {
	walk.TableModelBase
	rows []app.ServiceInfo
}

func (m *svcModel) RowCount() int { return len(m.rows) }

func (m *svcModel) Value(row, col int) interface{} {
	s := m.rows[row]
	switch col {
	case 0:
		return s.Name
	case 1:
		if s.Running {
			return "Running"
		}
		return "Stopped"
	case 2:
		if s.Port > 0 {
			return fmt.Sprint(s.Port)
		}
		return ""
	case 3:
		if s.PID > 0 {
			return fmt.Sprint(s.PID)
		}
	}
	return ""
}

// ---------------------------------------------------------------- the window

type window struct {
	b        Backend
	mw       *walk.MainWindow
	ni       *walk.NotifyIcon
	quitting atomic.Bool
	onQuit   func()

	state app.State
	have  bool

	status, jobTitle                               *walk.Label
	jobLog, healthText, logText                    *walk.TextEdit
	sites                                          *walk.TableView
	services                                       *walk.TableView
	siteM                                          *siteModel
	svcM                                           *svcModel
	btnUpAll, btnStopAll, btnInstall               *walk.PushButton
	btnStart, btnStop, btnOpen, btnPMA, btnImport  *walk.PushButton
	btnFolder, btnForget, btnPHP, btnCheck, btnFix *walk.PushButton
	phpBox, logBox                                 *walk.ComboBox
	chkSiteAuto, chkLogin                          *walk.CheckBox
	setInfo                                        *walk.TextEdit
	lastJobStatus                                  string
	lastLog                                        string
	lastLabel                                      string
	btnCancel                                      *walk.PushButton
	selected                                       string
	shownPHP, shownLogs                            []string
	ignoreToggle                                   bool
}

// Run shows the window (unless show is false) and the tray icon, and returns
// when the user chooses Quit. onQuit runs just before it returns.
func Run(b Backend, show bool, onQuit func()) error {
	runtime.LockOSThread()
	if _, err := walk.InitApp(); err != nil {
		return err
	}
	w := &window{b: b, onQuit: onQuit, siteM: &siteModel{}, svcM: &svcModel{}}
	if err := w.build(); err != nil {
		return err
	}
	fixControlIDs(w.mw.Handle())
	// Closing the window must not end the program: it hides to the tray (see tray()).
	// Quit exits through walk.App().Exit.
	w.mw.SetExitOnClose(false)
	if err := w.tray(); err != nil {
		return err
	}
	w.watchShowEvent()
	if show {
		w.mw.Show()
		_ = w.mw.Activate()
	}
	w.refresh()
	w.poll()
	walk.App().Run()
	if w.ni != nil {
		_ = w.ni.Dispose()
	}
	if w.onQuit != nil {
		w.onQuit()
	}
	return nil
}

func (w *window) build() error {
	return MainWindow{
		AssignTo: &w.mw,
		Title:    "Hyper Stack",
		MinSize:  Size{Width: 880, Height: 600},
		Size:     Size{Width: 960, Height: 640},
		Layout:   VBox{Margins: Margins{Left: 10, Top: 10, Right: 10, Bottom: 10}},
		Children: []Widget{
			Composite{
				Layout: HBox{Margins: Margins{Left: 0, Top: 6, Right: 0, Bottom: 6}},
				Children: []Widget{
					Label{AssignTo: &w.status, Text: "Loading..."},
					HSpacer{},
					PushButton{AssignTo: &w.btnInstall, MinSize: Size{Width: 100, Height: 30}, Text: "Install...", Visible: false, OnClicked: w.install},
					PushButton{AssignTo: &w.btnUpAll, MinSize: Size{Width: 100, Height: 30}, Text: "Start all", OnClicked: w.startAll},
					PushButton{AssignTo: &w.btnStopAll, MinSize: Size{Width: 100, Height: 30}, Text: "Stop all", OnClicked: w.stopAll},
					PushButton{MinSize: Size{Width: 130, Height: 30}, Text: "Open www folder", OnClicked: func() { w.run("Open www folder", func(a *app.App) error { return a.OpenWWW() }) }},
				},
			},
			TabWidget{
				StretchFactor: 1,
				Pages: []TabPage{
					{Title: "Projects", Layout: HBox{}, Children: w.projectsPage()},
					{Title: "Services", Layout: VBox{}, Children: []Widget{
						TableView{
							AssignTo: &w.services, Model: w.svcM, ColumnsOrderable: false,
							Columns: []TableViewColumn{{Title: "Service", Width: 220}, {Title: "Status", Width: 120}, {Title: "Port", Width: 90}, {Title: "Process", Width: 90}},
						},
					}},
					{Title: "Health check", Layout: VBox{}, Children: []Widget{
						Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
							PushButton{AssignTo: &w.btnCheck, Text: "Check now", OnClicked: w.check},
							PushButton{AssignTo: &w.btnFix, Text: "Apply safe fixes", OnClicked: func() {
								w.run("Apply safe fixes", func(a *app.App) error { return a.Doctor(context.Background(), true) })
							}},
							HSpacer{},
						}},
						TextEdit{AssignTo: &w.healthText, ReadOnly: true, VScroll: true},
					}},
					{Title: "Logs", Layout: VBox{}, Children: []Widget{
						Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
							Label{Text: "File:"},
							ComboBox{AssignTo: &w.logBox, MinSize: Size{Width: 240}, OnCurrentIndexChanged: w.loadLog},
							PushButton{Text: "Refresh", OnClicked: w.loadLog},
							HSpacer{},
						}},
						TextEdit{AssignTo: &w.logText, ReadOnly: true, VScroll: true, HScroll: true},
					}},
					{Title: "Settings", Layout: VBox{}, Children: []Widget{
						CheckBox{AssignTo: &w.chkLogin, Text: "Start Hyper Stack when I sign in to Windows", OnClicked: w.toggleLogin},
						TextEdit{AssignTo: &w.setInfo, ReadOnly: true, VScroll: true},
						Label{Text: "To change ports, run: hyper config set http_port 8080  (or https_port, mysql_port), then restart the services."},
					}},
				},
			},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{AssignTo: &w.jobTitle, Text: ""},
				HSpacer{},
				PushButton{AssignTo: &w.btnCancel, Text: "Cancel this operation", Visible: false, OnClicked: func() { w.b.Jobs.Cancel() }},
			}},
			TextEdit{AssignTo: &w.jobLog, ReadOnly: true, VScroll: true, MinSize: Size{Height: 110}, MaxSize: Size{Height: 140}},
		},
	}.Create()
}

func (w *window) projectsPage() []Widget {
	return []Widget{
		TableView{
			AssignTo: &w.sites, Model: w.siteM, StretchFactor: 1, ColumnsOrderable: false,
			Columns: []TableViewColumn{
				{Title: "Project", Width: 150}, {Title: "Address", Width: 200}, {Title: "PHP", Width: 50},
				{Title: "Status", Width: 110}, {Title: "Auto-start", Width: 80},
			},
			OnCurrentIndexChanged: w.selectionChanged,
			OnItemActivated:       w.openSite,
		},
		Composite{
			Layout: VBox{MarginsZero: true},
			Children: []Widget{
				PushButton{AssignTo: &w.btnStart, Text: "Start", OnClicked: w.startSelected},
				PushButton{AssignTo: &w.btnStop, Text: "Stop", OnClicked: w.stopSelected},
				PushButton{AssignTo: &w.btnOpen, Text: "Open site", OnClicked: w.openSite},
				PushButton{AssignTo: &w.btnPMA, Text: "phpMyAdmin", OnClicked: w.openPMA},
				PushButton{AssignTo: &w.btnImport, Text: "Import SQL...", OnClicked: w.importSQL},
				PushButton{AssignTo: &w.btnFolder, Text: "Open folder", OnClicked: w.openFolder},
				Label{Text: "PHP version:"},
				ComboBox{AssignTo: &w.phpBox, MinSize: Size{Width: 120}},
				PushButton{AssignTo: &w.btnPHP, Text: "Switch PHP", OnClicked: w.switchPHP},
				CheckBox{AssignTo: &w.chkSiteAuto, Text: "Start with Hyper Stack", OnClicked: w.toggleSiteAuto},
				PushButton{AssignTo: &w.btnForget, Text: "Remove from list", OnClicked: w.forget},
				VSpacer{},
			},
		},
	}
}

// ---------------------------------------------------------------- tray

func (w *window) tray() error {
	ni, err := walk.NewNotifyIcon()
	if err != nil {
		return err
	}
	w.ni = ni
	if ic := loadIcon(); ic != nil {
		_ = ni.SetIcon(ic)
		_ = w.mw.SetIcon(ic)
	}
	_ = ni.SetToolTip("Hyper Stack")
	add := func(text string, fn func()) {
		a := walk.NewAction()
		_ = a.SetText(text)
		a.Triggered().Attach(fn)
		_ = ni.ContextMenu().Actions().Add(a)
	}
	add("Open Hyper Stack", w.showWindow)
	add("Open in the browser", w.openWeb)
	add("Stop all services", w.stopAll)
	sep := walk.NewSeparatorAction()
	_ = ni.ContextMenu().Actions().Add(sep)
	add("Quit", w.quit)
	ni.MouseDown().Attach(func(_, _ int, button walk.MouseButton) {
		if button == walk.LeftButton {
			w.showWindow()
		}
	})
	w.mw.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		if w.quitting.Load() {
			return
		}
		*canceled = true // closing the window keeps the servers (and the tray icon) running
		w.mw.Hide()
	})
	return ni.SetVisible(true)
}

func loadIcon() *walk.Icon {
	f, err := os.CreateTemp("", "hyper-stack-*.ico")
	if err != nil {
		return nil
	}
	name := f.Name()
	_, werr := f.Write(tray.Icon())
	_ = f.Close()
	defer func() { _ = os.Remove(name) }()
	if werr != nil {
		return nil
	}
	ic, err := walk.NewIconFromFile(name)
	if err != nil {
		return nil
	}
	return ic
}

func (w *window) showWindow() {
	walk.App().Synchronize(func() {
		w.mw.Show()
		_ = w.mw.SetFocus()
		_ = w.mw.Activate()
		w.refresh()
	})
}

func (w *window) quit() {
	w.quitting.Store(true)
	walk.App().Synchronize(func() { walk.App().Exit(0) })
}

func (w *window) openWeb() {
	if w.b.WebURL != nil && w.b.OpenURL != nil {
		_ = w.b.OpenURL(w.b.WebURL())
	}
}

// watchShowEvent lets a second "hyper ui" bring this window to the front.
func (w *window) watchShowEvent() {
	name, err := windows.UTF16PtrFromString(showEventName())
	if err != nil {
		return
	}
	h, err := windows.CreateEvent(nil, 0, 0, name)
	if err != nil {
		return
	}
	go func() {
		for !w.quitting.Load() {
			if ev, _ := windows.WaitForSingleObject(h, 1000); ev == windows.WAIT_OBJECT_0 {
				w.showWindow()
			}
		}
		_ = windows.CloseHandle(h)
	}()
}

// ---------------------------------------------------------------- state

// refresh reads the state in the background and shows it.
func (w *window) refresh() {
	go func() {
		a, err := w.b.NewApp(io.Discard)
		if err != nil {
			walk.App().Synchronize(func() { _ = w.status.SetText("Could not read the state: " + err.Error()) })
			return
		}
		st := a.State(context.Background())
		a.Close()
		walk.App().Synchronize(func() { w.apply(st) })
	}()
}

func (w *window) apply(st app.State) {
	w.state, w.have = st, true
	sel := w.selected
	w.siteM.rows = st.Sites
	w.siteM.PublishRowsReset()
	w.svcM.rows = st.Services
	w.svcM.PublishRowsReset()
	for i, s := range st.Sites {
		if s.Name == sel {
			_ = w.sites.SetCurrentIndex(i)
		}
	}
	switch {
	case !st.Configured || !st.Installed:
		_ = w.status.SetText("Hyper Stack is not installed yet. Press Install.")
	case st.AnyRunning:
		online := 0
		for _, s := range st.Sites {
			if s.Online {
				online++
			}
		}
		_ = w.status.SetText(fmt.Sprintf("Running: %d project(s) online. Projects live in %s", online, st.WWW))
	default:
		_ = w.status.SetText(fmt.Sprintf("Everything is stopped. Projects live in %s", st.WWW))
	}
	w.btnInstall.SetVisible(!st.Installed)
	w.btnUpAll.SetEnabled(st.Installed)
	w.btnStopAll.SetEnabled(st.Installed)
	if !slices.Equal(w.shownPHP, st.PHPVersions) {
		_ = w.phpBox.SetModel(st.PHPVersions)
		w.shownPHP = st.PHPVersions
	}
	w.ignoreToggle = true
	w.chkLogin.SetChecked(st.LoginStart)
	w.ignoreToggle = false
	_ = w.setInfo.SetText(settingsText(st))
	if !slices.Equal(w.shownLogs, st.LogFiles) {
		_ = w.logBox.SetModel(st.LogFiles)
		w.shownLogs = st.LogFiles
		if len(st.LogFiles) > 0 {
			_ = w.logBox.SetCurrentIndex(0)
		}
	}
	if w.sites.CurrentIndex() < 0 && len(st.Sites) > 0 {
		_ = w.sites.SetCurrentIndex(0)
	}
	w.selectionChanged()
}

func settingsText(st app.State) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Install folder: %s\r\n", st.DataDir)
	fmt.Fprintf(&sb, "Projects folder (www): %s\r\n", st.WWW)
	fmt.Fprintf(&sb, "Web ports: http %d, https %d\r\n", st.Settings.HTTPPort, st.Settings.HTTPSPort)
	fmt.Fprintf(&sb, "MySQL: 127.0.0.1:%d, user root, no password\r\n", st.Settings.MySQLPort)
	fmt.Fprintf(&sb, "phpMyAdmin: %s\r\n", st.PMAURL)
	fmt.Fprintf(&sb, "Hosts file writable: %v\r\n", st.HostsWrite)
	return sb.String()
}

func (w *window) current() (app.SiteInfo, bool) {
	i := w.sites.CurrentIndex()
	if i < 0 || i >= len(w.siteM.rows) {
		return app.SiteInfo{}, false
	}
	return w.siteM.rows[i], true
}

func (w *window) selectionChanged() {
	s, ok := w.current()
	if ok {
		w.selected = s.Name
	}
	busy := w.busy()
	for _, b := range []*walk.PushButton{w.btnStart, w.btnStop, w.btnOpen, w.btnPMA, w.btnImport, w.btnFolder, w.btnPHP, w.btnForget} {
		b.SetEnabled(ok && w.state.Installed)
	}
	w.btnStart.SetEnabled(ok && w.state.Installed && !busy)
	w.btnStop.SetEnabled(ok && s.Online && !busy)
	w.btnOpen.SetEnabled(ok && s.Online)
	w.btnForget.SetEnabled(ok && !s.Discovered && !busy)
	w.chkSiteAuto.SetEnabled(ok && w.state.Installed)
	if ok {
		w.ignoreToggle = true
		w.chkSiteAuto.SetChecked(s.Autostart)
		w.ignoreToggle = false
		for i, v := range w.state.PHPVersions {
			if v == s.PHP {
				_ = w.phpBox.SetCurrentIndex(i)
			}
		}
	}
}

func (w *window) busy() bool {
	_, st, _, _ := w.b.Jobs.Snapshot()
	return st == "running"
}

// ---------------------------------------------------------------- jobs

// poll shows the progress of the running operation and refreshes when it ends.
func (w *window) poll() {
	go func() {
		for !w.quitting.Load() {
			time.Sleep(500 * time.Millisecond)
			title, st, errText, log := w.b.Jobs.Snapshot()
			walk.App().Synchronize(func() { w.showJob(title, st, errText, log) })
		}
	}()
	go func() {
		for !w.quitting.Load() {
			time.Sleep(4 * time.Second)
			if w.mw.Visible() {
				w.refresh()
			}
		}
	}()
}

func (w *window) showJob(title, st, errText, log string) {
	label := ""
	switch st {
	case "running":
		label = title + "  (working...)"
	case "done":
		label = title + "  (done)"
	case "failed":
		label = title + "  (failed): " + firstLine(errText)
	}
	// Only touch the panel when the operation changed, so a note about a quick
	// action (Open site...) is not overwritten by the previous operation.
	if label != w.lastLabel {
		w.lastLabel = label
		_ = w.jobTitle.SetText(label)
	}
	if log != w.lastLog {
		w.lastLog = log
		_ = w.jobLog.SetText(strings.ReplaceAll(strings.ReplaceAll(log, "\r\n", "\n"), "\n", "\r\n"))
		n := len(w.jobLog.Text())
		w.jobLog.SetTextSelection(n, n)
		w.jobLog.ScrollToCaret()
	}
	w.btnCancel.SetVisible(st == "running")
	if st != w.lastJobStatus {
		prev := w.lastJobStatus
		w.lastJobStatus = st
		w.selectionChanged()
		if prev == "running" {
			w.refresh()
			if st == "failed" {
				msgBox(w.mw, title, "That did not work:\n\n"+errText, walk.MsgBoxIconWarning)
			}
		}
	}
}

// pollNow shows the operation that just started without waiting for the next tick.
func (w *window) pollNow() {
	title, st, errText, log := w.b.Jobs.Snapshot()
	w.showJob(title, st, errText, log)
}

// note shows a short message about a quick action (one that is not a background operation).
func (w *window) note(title, text string) {
	_ = w.jobTitle.SetText(title)
	_ = w.jobLog.SetText(text)
}

func firstLine(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// run starts an operation on a fresh App.
func (w *window) run(title string, fn func(a *app.App) error) {
	err := w.b.Jobs.Start(title, func(ctx context.Context, out io.Writer) error {
		a, err := w.b.NewApp(out)
		if err != nil {
			return err
		}
		defer a.Close()
		return fn(a)
	})
	if err != nil {
		msgBox(w.mw, "Hyper Stack", err.Error(), walk.MsgBoxIconInformation)
		return
	}
	w.pollNow()
}

// runCtx is run for operations that take a context.
func (w *window) runCtx(title string, fn func(ctx context.Context, a *app.App) error) {
	err := w.b.Jobs.Start(title, func(ctx context.Context, out io.Writer) error {
		a, err := w.b.NewApp(out)
		if err != nil {
			return err
		}
		defer a.Close()
		return fn(ctx, a)
	})
	if err != nil {
		msgBox(w.mw, "Hyper Stack", err.Error(), walk.MsgBoxIconInformation)
		return
	}
	w.pollNow()
}

// ---------------------------------------------------------------- actions

func (w *window) startAll() {
	w.runCtx("Start all projects", func(ctx context.Context, a *app.App) error { return a.UpAll(ctx) })
}

func (w *window) stopAll() {
	w.runCtx("Stop all services", func(ctx context.Context, a *app.App) error { return a.Down(ctx, "", true) })
}

func (w *window) startSelected() {
	s, ok := w.current()
	if !ok {
		return
	}
	a, err := w.b.NewApp(io.Discard)
	if err != nil {
		msgBox(w.mw, "Hyper Stack", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	ins, err := a.Inspect(s.Root)
	a.Close()
	if err != nil {
		msgBox(w.mw, "Hyper Stack", err.Error(), walk.MsgBoxIconWarning)
		return
	}
	if ins.NeedsConfirm {
		msg := "This project wants to run these commands the first time it starts:\n\n  " + strings.Join(ins.Setup, "\n  ") +
			"\n\nOnly continue if you trust this project. Run them?"
		if msgBox(w.mw, "Start "+ins.Name, msg, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
			return
		}
	}
	w.runCtx("Start "+ins.Name, func(ctx context.Context, a *app.App) error { return a.Up(ctx, ins.Root) })
}

func (w *window) stopSelected() {
	if s, ok := w.current(); ok {
		w.runCtx("Stop "+s.Name, func(ctx context.Context, a *app.App) error { return a.StopSite(ctx, s.Name) })
	}
}

func (w *window) openSite() {
	s, ok := w.current()
	if !ok || s.URL == "" || w.b.OpenURL == nil {
		return
	}
	if !s.Online {
		w.note("Open site", s.Name+" is not running. Press Start first.")
		return
	}
	if err := w.b.OpenURL(s.URL); err != nil {
		w.note("Open site (failed)", err.Error())
		return
	}
	w.note("Open site", "Opened "+s.URL+" in your browser.")
}

func (w *window) openPMA() {
	if s, ok := w.current(); ok {
		w.runCtx("Open phpMyAdmin for "+s.Name, func(ctx context.Context, a *app.App) error { return a.DBOpen(ctx, s.Root) })
	}
}

func (w *window) openFolder() {
	if s, ok := w.current(); ok {
		if err := exec.Command("explorer", s.Root).Start(); err != nil {
			w.note("Open folder (failed)", err.Error())
			return
		}
		w.note("Open folder", "Opened "+s.Root)
	}
}

func (w *window) forget() {
	s, ok := w.current()
	if !ok {
		return
	}
	msg := fmt.Sprintf("Remove %s from Hyper Stack?\n\nIts files and database are not deleted.", s.Name)
	if msgBox(w.mw, "Remove "+s.Name, msg, walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) == walk.DlgCmdYes {
		w.runCtx("Remove "+s.Name, func(ctx context.Context, a *app.App) error { return a.ForgetSite(ctx, s.Name) })
	}
}

func (w *window) importSQL() {
	s, ok := w.current()
	if !ok {
		return
	}
	dlg := walk.FileDialog{Title: "Choose a SQL dump for " + s.Name, Filter: "SQL dumps (*.sql;*.sql.gz;*.gz)|*.sql;*.sql.gz;*.gz|All files (*.*)|*.*"}
	if ok, _ := dlg.ShowOpen(w.mw); !ok || dlg.FilePath == "" {
		return
	}
	file := dlg.FilePath
	fresh := msgBox(w.mw, "Import "+filepath.Base(file),
		"Replace the database?\n\nYes: empty "+s.Database+" first (a backup is saved), then import.\nNo: add the dump on top of what is there.",
		walk.MsgBoxYesNoCancel|walk.MsgBoxIconQuestion)
	if fresh == walk.DlgCmdCancel {
		return
	}
	w.runCtx("Import "+filepath.Base(file)+" into "+s.Name, func(ctx context.Context, a *app.App) error {
		return a.DBImport(ctx, s.Root, file, fresh == walk.DlgCmdYes)
	})
}

func (w *window) switchPHP() {
	s, ok := w.current()
	i := w.phpBox.CurrentIndex()
	if !ok || i < 0 || i >= len(w.state.PHPVersions) {
		return
	}
	v := w.state.PHPVersions[i]
	if v == s.PHP {
		return
	}
	w.runCtx("Switch "+s.Name+" to PHP "+v, func(ctx context.Context, a *app.App) error { return a.SetProjectPHP(ctx, s.Root, v) })
}

func (w *window) toggleSiteAuto() {
	if w.ignoreToggle {
		return
	}
	s, ok := w.current()
	if !ok {
		return
	}
	a, err := w.b.NewApp(io.Discard)
	if err == nil {
		err = a.SetProjectAutostart(s.Root, w.chkSiteAuto.Checked())
		a.Close()
	}
	if err != nil {
		msgBox(w.mw, "Hyper Stack", err.Error(), walk.MsgBoxIconWarning)
	}
	w.refresh()
}

func (w *window) toggleLogin() {
	if w.ignoreToggle {
		return
	}
	a, err := w.b.NewApp(io.Discard)
	if err == nil {
		err = a.SetLoginAutostart(w.chkLogin.Checked())
		a.Close()
	}
	if err != nil {
		msgBox(w.mw, "Hyper Stack", err.Error(), walk.MsgBoxIconWarning)
	}
	w.refresh()
}

func (w *window) check() {
	_ = w.healthText.SetText("Checking...")
	go func() {
		a, err := w.b.NewApp(io.Discard)
		if err != nil {
			walk.App().Synchronize(func() { _ = w.healthText.SetText(err.Error()) })
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
			fmt.Fprintf(&sb, "%s %s\r\n", mark, f.Title)
			if !f.OK && f.Detail != "" {
				fmt.Fprintf(&sb, "         %s\r\n", f.Detail)
			}
			if !f.OK && f.Fix != "" {
				fmt.Fprintf(&sb, "         Fix: %s\r\n", f.Fix)
			}
		}
		walk.App().Synchronize(func() { _ = w.healthText.SetText(sb.String()) })
	}()
}

func (w *window) loadLog() {
	i := w.logBox.CurrentIndex()
	if i < 0 || i >= len(w.state.LogFiles) {
		return
	}
	name := w.state.LogFiles[i]
	go func() {
		a, err := w.b.NewApp(io.Discard)
		if err != nil {
			return
		}
		txt, err := a.TailLog(name, 300)
		a.Close()
		if err != nil {
			txt = err.Error()
		}
		txt = strings.ReplaceAll(strings.ReplaceAll(txt, "\r\n", "\n"), "\n", "\r\n")
		walk.App().Synchronize(func() {
			_ = w.logText.SetText(txt)
			n := len(txt)
			w.logText.SetTextSelection(n, n)
			w.logText.ScrollToCaret()
		})
	}()
}

// ---------------------------------------------------------------- install

func (w *window) install() {
	if !w.have {
		return
	}
	dir := w.state.DataDir
	if !w.state.Configured {
		dir = w.state.DefaultDir
	}
	st := w.state.Settings
	if !w.state.Installed {
		st.HTTPPort, st.HTTPSPort, st.MySQLPort, _ = suggestPorts(st.HTTPPort, st.HTTPSPort, st.MySQLPort)
	}
	var dlg *walk.Dialog
	var dirEdit *walk.LineEdit
	var httpN, httpsN, mysqlN *walk.NumberEdit
	accepted := false
	var chosen string
	var ports app.Ports
	err := Dialog{
		AssignTo: &dlg, Title: "Install Hyper Stack", MinSize: Size{Width: 520, Height: 260},
		Layout: VBox{},
		Children: []Widget{
			Label{Text: "Choose one folder. Everything goes inside it, with a www folder for your projects.\nUse a short path with plain English letters, for example C:\\HyperStack or D:\\Hyper."},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				LineEdit{AssignTo: &dirEdit, Text: dir, ReadOnly: w.state.Configured},
				PushButton{Text: "Browse...", Enabled: !w.state.Configured, OnClicked: func() {
					f := walk.FileDialog{Title: "Choose the install folder", InitialDirPath: dirEdit.Text()}
					if ok, _ := f.ShowBrowseFolder(dlg); ok && f.FilePath != "" {
						_ = dirEdit.SetText(f.FilePath)
					}
				}},
			}},
			GroupBox{Title: "Ports (change these only if Laragon or XAMPP is running)", Layout: Grid{Columns: 4}, Children: []Widget{
				Label{Text: "http"}, NumberEdit{AssignTo: &httpN, Value: float64(st.HTTPPort), MinValue: 1, MaxValue: 65535},
				Label{Text: "https"}, NumberEdit{AssignTo: &httpsN, Value: float64(st.HTTPSPort), MinValue: 1, MaxValue: 65535},
				Label{Text: "mysql"}, NumberEdit{AssignTo: &mysqlN, Value: float64(st.MySQLPort), MinValue: 1, MaxValue: 65535},
			}},
			Label{Text: "It downloads about 300 MB and asks for administrator permission once, to trust the local certificate and edit the hosts file."},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				HSpacer{},
				PushButton{Text: "Install", OnClicked: func() {
					// Read the fields now: they are gone once the dialog closes.
					dirText := strings.TrimSpace(dirEdit.Text())
					if !w.state.Configured {
						abs, err := config.ValidateInstallDir(dirText)
						if err != nil {
							msgBox(dlg, "Install Hyper Stack", err.Error(), walk.MsgBoxIconWarning)
							return // keep the dialog open so the folder can be changed
						}
						dirText = abs
					}
					chosen = dirText
					ports = app.Ports{HTTP: int(httpN.Value()), HTTPS: int(httpsN.Value()), MySQL: int(mysqlN.Value())}
					accepted = true
					dlg.Accept()
				}},
				PushButton{Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}.Create(w.mw)
	if err != nil {
		return
	}
	fixControlIDs(dlg.Handle())
	dlg.Run()
	if !accepted {
		return
	}
	w.runCtx("Install Hyper Stack", func(ctx context.Context, a *app.App) error { return a.InstallTo(ctx, chosen, ports) })
}

// msgBox shows a simple message box.
func msgBox(owner walk.Form, title, text string, style walk.MsgBoxStyle) int {
	return walk.MsgBox(owner, title, text, style) //nolint:staticcheck // the classic box is all that is needed here
}

// fixControlIDs gives every button and combo box under root a unique control ID.
// walk leaves the controls on tab pages with ID 0, and a click (WM_COMMAND) finds
// its sender by ID: every one of those buttons then ran the first button's action
// (Stop, Open site, phpMyAdmin... all started the project).
func fixControlIDs(root win.HWND) {
	var all []win.HWND
	collect := syscall.NewCallback(func(h win.HWND, _ uintptr) uintptr {
		all = append(all, h)
		return 1
	})
	win.EnumChildWindows(root, collect, 0)
	used := map[int32]bool{}
	for _, h := range all {
		used[win.GetWindowLong(h, win.GWL_ID)] = true
	}
	next := int32(5000)
	for _, h := range all {
		if win.GetWindowLong(h, win.GWL_ID) != 0 {
			continue
		}
		var buf [64]uint16
		n, _ := windows.GetClassName(windows.HWND(h), &buf[0], int32(len(buf)))
		switch strings.ToLower(windows.UTF16ToString(buf[:n])) {
		case "button", "combobox":
			for used[next] {
				next++
			}
			win.SetWindowLong(h, win.GWL_ID, next)
			used[next] = true
		}
	}
}
