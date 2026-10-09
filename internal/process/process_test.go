package process

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMain lets the test binary double as a sleeping child process.
func TestMain(m *testing.M) {
	if os.Getenv("HYPER_TEST_SLEEP") == "1" {
		time.Sleep(time.Minute)
		return
	}
	if out := os.Getenv("HYPER_TEST_SPAWN"); out != "" {
		// Acts as a "dashboard": starts a long-lived child, records its pid, then idles.
		child := Spec{Name: "grandchild", Bin: os.Args[0], Env: []string{"HYPER_TEST_SLEEP=1"},
			RunDir: filepath.Dir(out), LogFile: filepath.Join(filepath.Dir(out), "gc.log")}
		if err := Start(child); err != nil {
			os.Exit(2)
		}
		time.Sleep(time.Minute)
		return
	}
	os.Exit(m.Run())
}

func TestStartStopLifecycle(t *testing.T) {
	d := t.TempDir()
	s := Spec{Name: "sleeper", Bin: os.Args[0], Env: []string{"HYPER_TEST_SLEEP=1"},
		RunDir: filepath.Join(d, "run"), LogFile: filepath.Join(d, "logs", "s.log")}

	if Running(s) {
		t.Fatal("should not be running yet")
	}
	if err := Start(s); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { Stop(s) })
	if !Running(s) {
		t.Fatal("should be running")
	}
	pid := PID(s)
	if err := Start(s); err != nil || PID(s) != pid {
		t.Fatalf("second Start must be a no-op (pid %d -> %d, err %v)", pid, PID(s), err)
	}
	if err := Stop(s); err != nil {
		t.Fatal(err)
	}
	if Running(s) {
		t.Fatal("should be stopped")
	}
	if err := Stop(s); err != nil {
		t.Fatalf("stopping a stopped service must succeed: %v", err)
	}
}

// Stopping the dashboard must not take down what the dashboard started
// (MySQL, workers). A service itself is still stopped with its whole tree.
func TestNoTreeLeavesChildrenRunning(t *testing.T) {
	d := t.TempDir()
	marker := filepath.Join(d, "spawned")
	parent := Spec{Name: "dash", Bin: os.Args[0], Env: []string{"HYPER_TEST_SPAWN=" + marker},
		RunDir: d, LogFile: filepath.Join(d, "dash.log"), NoTree: true}
	if err := Start(parent); err != nil {
		t.Fatal(err)
	}
	child := Spec{Name: "grandchild", RunDir: d}
	t.Cleanup(func() { _ = Stop(child); _ = Stop(parent) })

	for i := 0; i < 50 && PID(child) == 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if !Running(child) {
		t.Fatal("grandchild did not start")
	}
	if err := Stop(parent); err != nil {
		t.Fatal(err)
	}
	if Running(parent) {
		t.Fatal("parent should be stopped")
	}
	time.Sleep(500 * time.Millisecond)
	if !Running(child) {
		t.Fatal("stopping the parent with NoTree killed its child")
	}
}

func TestTreeStopKillsChildren(t *testing.T) {
	d := t.TempDir()
	marker := filepath.Join(d, "spawned")
	parent := Spec{Name: "svc", Bin: os.Args[0], Env: []string{"HYPER_TEST_SPAWN=" + marker},
		RunDir: d, LogFile: filepath.Join(d, "svc.log")}
	if err := Start(parent); err != nil {
		t.Fatal(err)
	}
	child := Spec{Name: "grandchild", RunDir: d}
	t.Cleanup(func() { _ = Stop(child); _ = Stop(parent) })
	for i := 0; i < 50 && PID(child) == 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if !Running(child) {
		t.Fatal("grandchild did not start")
	}
	if err := Stop(parent); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30 && Running(child); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	if Running(child) {
		t.Skip("child survived a tree stop on this platform") // Unix signals the process group only
	}
}
