package direct

import (
	"strings"
	"testing"

	"github.com/hyperdesign/hyper-stack/internal/download"
)

const feed = `{
 "8.3": {"version":"8.3.35","nts-vs16-x64":{"zip":{"path":"php-8.3.35-nts-Win32-vs16-x64.zip","sha256":"aa"}}},
 "8.4": {"version":"8.4.26","nts-vs17-x64":{"zip":{"path":"php-8.4.26-nts-Win32-vs17-x64.zip","sha256":"bb"}}}
}`

func TestPHPFromReleases(t *testing.T) {
	a, err := phpFromReleases([]byte(feed), "8.4")
	if err != nil {
		t.Fatal(err)
	}
	if a.Checksum != "bb" || a.URL != "https://downloads.php.net/~windows/releases/php-8.4.26-nts-Win32-vs17-x64.zip" {
		t.Fatalf("got %+v", a)
	}
	if a, err = phpFromReleases([]byte(feed), "8.3"); err != nil || a.Checksum != "aa" {
		t.Fatalf("vs16 fallback failed: %+v %v", a, err)
	}
	if _, err = phpFromReleases([]byte(feed), "8.2"); err == nil {
		t.Fatal("expected error for missing version")
	}
}

func TestChecksumFor(t *testing.T) {
	sums := "abc123  caddy_2_mac_arm64.tar.gz\ndef456  caddy_2_mac_amd64.tar.gz\n"
	got, err := checksumFor(sums, "caddy_2_mac_amd64.tar.gz")
	if err != nil || got != "def456" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := checksumFor(sums, "nope"); err == nil {
		t.Fatal("expected error")
	}
}

// php.net publishes no checksums for archived Windows builds, so ours are pinned.
func TestLegacyPHPIsPinnedAndNeedsNoNetwork(t *testing.T) {
	for version, want := range map[string]string{
		"7.4": "php-7.4.33-nts-Win32-vc15-x64.zip", "8.0": "php-8.0.30-nts-Win32-vs16-x64.zip", "8.1": "php-8.1.34-nts-Win32-vs16-x64.zip",
	} {
		a, ok := legacyPHP(version)
		if !ok {
			t.Fatalf("PHP %s must be available", version)
		}
		if !strings.HasSuffix(a.URL, "/archives/"+want) {
			t.Errorf("PHP %s: wrong file %s", version, a.URL)
		}
		if len(a.Checksum) != 64 || a.Algo != download.SHA256 {
			t.Errorf("PHP %s must carry a SHA-256, got %q", version, a.Checksum)
		}
		if a.StripTop {
			t.Errorf("PHP %s zips have no top folder", version)
		}
	}
	if _, ok := legacyPHP("8.3"); ok {
		t.Error("current versions come from the live release feed, not the archive table")
	}
}
