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
