// Package direct is the Runtime that downloads and manages official binaries itself.
package direct

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hyperdesign/hyper-stack/internal/download"
)

// Artifact is one downloadable archive with its checksum.
type Artifact struct {
	Name     string
	URL      string
	Algo     download.Algo
	Checksum string
	StripTop bool // archive has a single top-level folder to drop
}

// Pinned versions. Bump deliberately; checksums are verified on every install.
const (
	NginxVersion  = "1.30.5"
	nginxSHA256   = "e5afe28b6a50bec92c478bfe1a4d3758206b80fb77159277bc5c4e88955c2a35"
	MySQLVersion  = "8.0.46"
	mysqlWinMD5   = "003f527d5df61b663ff191038cd676bd" // from dev.mysql.com, which publishes MD5 only
	CaddyVersion  = "2.11.7"
	phpReleasesJS = "https://downloads.php.net/~windows/releases/releases.json"
)

// Nginx returns the Windows nginx artifact (no official macOS binary exists).
func Nginx() Artifact {
	return Artifact{
		Name: "nginx-" + NginxVersion, URL: "https://nginx.org/download/nginx-" + NginxVersion + ".zip",
		Algo: download.SHA256, Checksum: nginxSHA256, StripTop: true,
	}
}

// MySQLWindows returns the MySQL zip for Windows x64.
func MySQLWindows() Artifact {
	f := "mysql-" + MySQLVersion + "-winx64.zip"
	return Artifact{
		Name: "mysql-" + MySQLVersion, URL: "https://cdn.mysql.com/Downloads/MySQL-8.0/" + f,
		Algo: download.MD5, Checksum: mysqlWinMD5, StripTop: true,
	}
}

// PHPWindows resolves the latest NTS x64 zip for a minor version ("8.3") from
// the official releases feed, including its SHA-256.
func PHPWindows(ctx context.Context, version string) (Artifact, error) {
	if a, ok := legacyPHP(version); ok {
		return a, nil
	}
	body, err := httpGet(ctx, phpReleasesJS)
	if err != nil {
		return Artifact{}, err
	}
	return phpFromReleases(body, version)
}

// phpArchives is where php.net keeps builds that are no longer in the release feed.
const phpArchives = "https://downloads.php.net/~windows/releases/archives/"

// legacyBuilds are the end-of-life PHP versions offered so old projects can run
// and be upgraded step by step. php.net publishes no checksums for archived
// builds, but these releases are final and can never change, so each file's
// SHA-256 is pinned here (taken from the php.net download over HTTPS) and
// verified on every install.
var legacyBuilds = map[string]struct{ file, sha256 string }{
	"7.4": {"php-7.4.33-nts-Win32-vc15-x64.zip", "14ae3250d4447c8ccfc4c45a70d90adfbcd61e728d85f0be56a7ddf8f9c8aace"},
	"8.0": {"php-8.0.30-nts-Win32-vs16-x64.zip", "dfb70498ffa2c617f2f655a155564697e3c9cca41709938fd1a5997d1d5b0785"},
	"8.1": {"php-8.1.34-nts-Win32-vs16-x64.zip", "9cfe246cb144076c16f5913a3ef88a474c3dd7e60f0f0c8bb95faf68674016cc"},
}

func legacyPHP(minor string) (Artifact, bool) {
	b, ok := legacyBuilds[minor]
	if !ok {
		return Artifact{}, false
	}
	return Artifact{Name: "php-" + minor, URL: phpArchives + b.file, Algo: download.SHA256, Checksum: b.sha256}, true
}

func phpFromReleases(body []byte, minor string) (Artifact, error) {
	var feed map[string]json.RawMessage
	if err := json.Unmarshal(body, &feed); err != nil {
		return Artifact{}, fmt.Errorf("php release feed is not valid JSON: %w", err)
	}
	raw, ok := feed[minor]
	if !ok {
		return Artifact{}, fmt.Errorf("PHP %s is not in the official Windows release feed", minor)
	}
	var rel map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rel); err != nil {
		return Artifact{}, err
	}
	for _, key := range []string{"nts-vs17-x64", "nts-vs16-x64"} {
		b, ok := rel[key]
		if !ok {
			continue
		}
		var build struct {
			Zip struct {
				Path   string `json:"path"`
				SHA256 string `json:"sha256"`
			} `json:"zip"`
		}
		if err := json.Unmarshal(b, &build); err != nil || build.Zip.Path == "" || build.Zip.SHA256 == "" {
			return Artifact{}, fmt.Errorf("PHP %s entry %s is incomplete in the release feed", minor, key)
		}
		return Artifact{
			Name: "php-" + minor, URL: "https://downloads.php.net/~windows/releases/" + build.Zip.Path,
			Algo: download.SHA256, Checksum: build.Zip.SHA256, StripTop: false,
		}, nil
	}
	return Artifact{}, fmt.Errorf("no NTS x64 build for PHP %s in the release feed", minor)
}

// CaddyMac resolves the macOS Caddy archive for goarch ("arm64" or "amd64")
// using the checksums file published with the release.
func CaddyMac(ctx context.Context, goarch string) (Artifact, error) {
	base := "https://github.com/caddyserver/caddy/releases/download/v" + CaddyVersion + "/"
	file := "caddy_" + CaddyVersion + "_mac_" + goarch + ".tar.gz"
	sums, err := httpGet(ctx, base+"caddy_"+CaddyVersion+"_checksums.txt")
	if err != nil {
		return Artifact{}, err
	}
	sum, err := checksumFor(string(sums), file)
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{Name: "caddy-" + CaddyVersion, URL: base + file, Algo: algoByLength(sum), Checksum: sum}, nil
}

// algoByLength tells SHA-512 from SHA-256 by the length of the hex digest (some
// vendors publish one, some the other).
func algoByLength(sum string) download.Algo {
	if len(sum) == 128 {
		return download.SHA512
	}
	return download.SHA256
}

// checksumFor reads "<sha256>  <file>" lines.
func checksumFor(sums, file string) (string, error) {
	sc := bufio.NewScanner(strings.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == file {
			return f[0], nil
		}
	}
	return "", fmt.Errorf("%s not found in the release checksums", file)
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w. Check your internet connection", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// Composer resolves the latest stable composer.phar with its published SHA-256.
func Composer(ctx context.Context) (Artifact, error) {
	const base = "https://getcomposer.org/download/latest-stable/composer.phar"
	sums, err := httpGet(ctx, base+".sha256sum")
	if err != nil {
		return Artifact{}, err
	}
	f := strings.Fields(string(sums))
	if len(f) < 1 {
		return Artifact{}, fmt.Errorf("composer checksum file is empty")
	}
	return Artifact{Name: "composer", URL: base, Algo: download.SHA256, Checksum: f[0]}, nil
}

// CABundle resolves curl's CA bundle (needed so PHP/Composer can verify HTTPS).
func CABundle(ctx context.Context) (Artifact, error) {
	const base = "https://curl.se/ca/cacert.pem"
	sums, err := httpGet(ctx, base+".sha256")
	if err != nil {
		return Artifact{}, err
	}
	f := strings.Fields(string(sums))
	if len(f) < 1 {
		return Artifact{}, fmt.Errorf("cacert checksum file is empty")
	}
	return Artifact{Name: "cacert", URL: base, Algo: download.SHA256, Checksum: f[0]}, nil
}

// NodeWindows resolves the newest Node release of a major version ("20").
func NodeWindows(ctx context.Context, major string) (Artifact, error) {
	base := "https://nodejs.org/dist/latest-v" + major + ".x/"
	sums, err := httpGet(ctx, base+"SHASUMS256.txt")
	if err != nil {
		return Artifact{}, err
	}
	sc := bufio.NewScanner(strings.NewReader(string(sums)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.HasSuffix(f[1], "-win-x64.zip") {
			return Artifact{Name: "node-" + major, URL: base + f[1], Algo: download.SHA256, Checksum: f[0], StripTop: true}, nil
		}
	}
	return Artifact{}, fmt.Errorf("no Windows x64 zip for Node %s in the official release list", major)
}

// PHPMyAdmin resolves the latest phpMyAdmin release.
func PHPMyAdmin(ctx context.Context) (Artifact, error) {
	body, err := httpGet(ctx, "https://www.phpmyadmin.net/home_page/version.json")
	if err != nil {
		return Artifact{}, err
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(body, &v); err != nil || v.Version == "" {
		return Artifact{}, fmt.Errorf("phpMyAdmin version feed is unreadable")
	}
	url := "https://files.phpmyadmin.net/phpMyAdmin/" + v.Version + "/phpMyAdmin-" + v.Version + "-all-languages.zip"
	sums, err := httpGet(ctx, url+".sha256")
	if err != nil {
		return Artifact{}, err
	}
	f := strings.Fields(string(sums))
	if len(f) < 1 {
		return Artifact{}, fmt.Errorf("phpMyAdmin checksum file is empty")
	}
	return Artifact{Name: "phpmyadmin-" + v.Version, URL: url, Algo: download.SHA256, Checksum: f[0], StripTop: true}, nil
}
