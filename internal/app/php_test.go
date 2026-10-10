package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, b)
	}
	return m
}

func TestWritePHPVersionKeepsEverythingElse(t *testing.T) {
	dir := t.TempDir()
	orig := "\xEF\xBB\xBF" + `{"name":"shop","php":"8.3","database":{"engine":"mysql","name":"shop_db"},"setup":["php -v"],"workers":["php artisan queue:work"]}`
	if err := os.WriteFile(filepath.Join(dir, "hyper.json"), []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePHPVersion(dir, "7.4"); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, filepath.Join(dir, "hyper.json"))
	if m["php"] != "7.4" || m["name"] != "shop" {
		t.Errorf("php/name wrong: %v", m)
	}
	if len(m["setup"].([]any)) != 1 || len(m["workers"].([]any)) != 1 || m["database"].(map[string]any)["name"] != "shop_db" {
		t.Errorf("other settings were lost: %v", m)
	}
}

func TestWritePHPVersionCreatesTheFileFromTheFolder(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "My Old App")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writePHPVersion(dir, "7.4"); err != nil {
		t.Fatal(err)
	}
	m := readJSON(t, filepath.Join(dir, "hyper.json"))
	if m["name"] != "my-old-app" || m["php"] != "7.4" || m["database"].(map[string]any)["name"] != "my_old_app" {
		t.Errorf("created file wrong: %v", m)
	}
}

func TestWritePHPVersionRefusesBrokenJSON(t *testing.T) {
	dir := t.TempDir()
	bad := `{"name": "x", `
	if err := os.WriteFile(filepath.Join(dir, "hyper.json"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writePHPVersion(dir, "8.1"); err == nil {
		t.Fatal("must not overwrite a file it cannot read")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "hyper.json")); string(b) != bad {
		t.Errorf("the file was changed: %q", b)
	}
}

func TestSetProjectPHPRefusesUnknownVersions(t *testing.T) {
	a, _ := newTestApp(t)
	dir := mkProject(t, a, "blog", "")
	for _, v := range []string{"5.6", "7.3", "9.0", "latest", ""} {
		err := a.SetProjectPHP(context.Background(), dir, v)
		if err == nil || !strings.Contains(err.Error(), "7.4") {
			t.Errorf("version %q must be refused and the choices listed: %v", v, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "hyper.json")); err == nil {
		t.Error("nothing may be written for a refused version")
	}
}

// Changing the version must not start commands nobody has reviewed.
func TestSetProjectPHPDoesNotRestartAProjectWithUnreviewedSetup(t *testing.T) {
	a, out := newTestApp(t)
	dir := mkProject(t, a, "legacy", `{"name":"legacy","php":"8.3","database":{"engine":"mysql","name":"legacy"},"setup":["php -v"]}`)
	if err := a.SetProjectPHP(context.Background(), dir, "7.4"); err != nil {
		t.Fatalf("saving the version is not a failure: %v", err)
	}
	if got := readJSON(t, filepath.Join(dir, "hyper.json"))["php"]; got != "7.4" {
		t.Errorf("version not saved: %v", got)
	}
	if !strings.Contains(out.String(), "was not restarted") {
		t.Errorf("it must say why it did not restart: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(a.P.Sites, "legacy.json")); err == nil {
		t.Error("the project must not have been started")
	}
}

func TestProjectPHPReportsWhereTheVersionCameFrom(t *testing.T) {
	a, _ := newTestApp(t)
	auto := mkProject(t, a, "oldapp", "")
	if err := os.WriteFile(filepath.Join(auto, "composer.json"), []byte(`{"require":{"php":"^7.1.3"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := a.ProjectPHP(auto)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "7.4" || !strings.Contains(info.Source, "detected") {
		t.Errorf("a Laravel 5.8 style project should be detected as 7.4: %+v", info)
	}
	if len(info.Available) != 6 || info.Available[0] != "7.4" {
		t.Errorf("available versions: %v", info.Available)
	}
}
