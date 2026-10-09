package direct

import "testing"

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
