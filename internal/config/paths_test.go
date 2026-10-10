package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultNotConfiguredThenPointer(t *testing.T) {
	t.Setenv("HYPER_HOME", filepath.Join(t.TempDir(), "home"))
	p, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if p.Configured {
		t.Fatal("fresh machine must not be configured")
	}

	root := filepath.Join(t.TempDir(), "Hyper")
	if err := SaveRoot(root); err != nil {
		t.Fatal(err)
	}
	p, _ = Default()
	if !p.Configured || p.Root != root || p.WWW != filepath.Join(root, "www") || p.Bin != filepath.Join(root, "bin") {
		t.Fatalf("pointer not honoured: %+v", p)
	}
	if p.Home == p.Root {
		t.Fatal("home must stay separate from the chosen root")
	}
}

func TestOlderInstallInHomeIsStillConfigured(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	t.Setenv("HYPER_HOME", home)
	_ = os.MkdirAll(filepath.Join(home, "bin", "nginx"), 0o755)
	_ = os.WriteFile(filepath.Join(home, "bin", "nginx", ".hyper-installed"), nil, 0o644)
	p, _ := Default()
	if !p.Configured || p.Root != home {
		t.Fatalf("older install not recognised: %+v", p)
	}
}

func TestValidateInstallDir(t *testing.T) {
	base := t.TempDir()
	good := filepath.Join(base, "HyperStack")
	if got, err := ValidateInstallDir(good); err != nil || got != good {
		t.Fatalf("new folder rejected: %v", err)
	}

	empty := filepath.Join(base, "empty")
	_ = os.MkdirAll(empty, 0o755)
	if _, err := ValidateInstallDir(empty); err != nil {
		t.Errorf("empty folder rejected: %v", err)
	}

	full := filepath.Join(base, "full")
	_ = os.MkdirAll(full, 0o755)
	_ = os.WriteFile(filepath.Join(full, "something.txt"), nil, 0o644)
	if _, err := ValidateInstallDir(full); err == nil {
		t.Error("a non-empty unrelated folder must be rejected")
	}

	mine := filepath.Join(base, "mine")
	p := layout(base, mine, true)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateInstallDir(mine); err != nil {
		t.Errorf("an existing Hyper Stack folder must be accepted: %v", err)
	}

	if _, err := ValidateInstallDir(filepath.Join(base, "مشاريع")); err == nil {
		t.Error("non-ASCII path must be rejected")
	}
	if _, err := ValidateInstallDir(""); err == nil {
		t.Error("empty input must be rejected")
	}
}

func TestEnsureCreatesWWWAndMarker(t *testing.T) {
	base := t.TempDir()
	p := layout(filepath.Join(base, "home"), filepath.Join(base, "root"), true)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{p.WWW, p.Bin, p.Certs, p.HomeRun} {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			t.Errorf("%s not created", d)
		}
	}
	if _, err := os.Stat(filepath.Join(p.Root, markerFile)); err != nil {
		t.Error("marker file missing")
	}
}

func TestSettingsKeepTheAutostartList(t *testing.T) {
	p := layout(t.TempDir(), t.TempDir(), true)
	if err := p.Ensure(); err != nil {
		t.Fatal(err)
	}
	s, _ := p.LoadSettings()
	if len(s.Autostart) != 0 || s.HTTPPort != 80 {
		t.Fatalf("defaults wrong: %+v", s)
	}
	s.Autostart = []string{`C:\HyperStack\www\blog`}
	s.HTTPPort = 8080
	if err := p.SaveSettings(s); err != nil {
		t.Fatal(err)
	}
	got, err := p.LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Autostart) != 1 || got.Autostart[0] != `C:\HyperStack\www\blog` || got.HTTPPort != 8080 || got.MySQLPort != 3306 {
		t.Fatalf("round trip lost data: %+v", got)
	}
}
