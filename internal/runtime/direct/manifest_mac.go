package direct

import (
	"bufio"
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/hyperdesign/hyper-stack/internal/download"
)

// macArch maps Go's GOARCH to how each vendor names the same processor.
type macArch struct{ caddy, mysql, node, static string }

func macArchFor(goarch string) (macArch, error) {
	switch goarch {
	case "arm64":
		return macArch{caddy: "arm64", mysql: "arm64", node: "arm64", static: "aarch64"}, nil
	case "amd64":
		return macArch{caddy: "amd64", mysql: "x86_64", node: "x64", static: "x86_64"}, nil
	}
	return macArch{}, fmt.Errorf("unsupported processor %q: Hyper Stack runs on Apple Silicon and Intel Macs", goarch)
}

// MySQLMacVersion is the MySQL release installed on macOS.
const MySQLMacVersion = "8.0.46"

// mysqlMacMD5 are the checksums dev.mysql.com publishes (it publishes MD5 only).
var mysqlMacMD5 = map[string]string{
	"arm64":  "aefb850c25a2c703a63554283fb94cae",
	"x86_64": "a86ee80b624c8574d56ebf0e5e75f8c3",
}

// MySQLMac returns the official macOS MySQL archive for a processor. Oracle ships
// these builds for macOS 15.
func MySQLMac(a macArch) Artifact {
	file := "mysql-" + MySQLMacVersion + "-macos15-" + a.mysql + ".tar.gz"
	return Artifact{
		Name: "mysql-" + MySQLMacVersion, URL: "https://cdn.mysql.com/Downloads/MySQL-8.0/" + file,
		Algo: download.MD5, Checksum: mysqlMacMD5[a.mysql], StripTop: true,
	}
}

// NodeMac resolves the newest Node release of a major version for macOS.
func NodeMac(ctx context.Context, major string, a macArch) (Artifact, error) {
	base := "https://nodejs.org/dist/latest-v" + major + ".x/"
	sums, err := httpGet(ctx, base+"SHASUMS256.txt")
	if err != nil {
		return Artifact{}, err
	}
	return nodeMacFrom(string(sums), base, major, a)
}

// nodeMacFrom picks the macOS archive out of Node's SHASUMS256.txt.
func nodeMacFrom(sums, base, major string, a macArch) (Artifact, error) {
	suffix := "-darwin-" + a.node + ".tar.gz"
	sc := bufio.NewScanner(strings.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.HasSuffix(f[1], suffix) {
			return Artifact{Name: "node-" + major, URL: base + f[1], Algo: download.SHA256, Checksum: f[0], StripTop: true}, nil
		}
	}
	return Artifact{}, fmt.Errorf("no macOS %s build for Node %s in the official release list", a.node, major)
}

const staticPHPBase = "https://dl.static-php.dev/static-php-cli/bulk/"

// StaticPHP returns the static command-line and FPM builds of a PHP version for
// macOS. static-php.dev publishes no checksums, so the files are pinned (see
// staticphp_pins.go) and verified on every install.
func StaticPHP(version string, a macArch) (cli, fpm Artifact, ok bool) {
	patch, known := staticPHPPatch[version]
	if !known {
		return Artifact{}, Artifact{}, false
	}
	mk := func(kind string) (Artifact, bool) {
		file := fmt.Sprintf("php-%s-%s-macos-%s.tar.gz", patch, kind, a.static)
		sum, pinned := staticPHPSHA256[file]
		if !pinned {
			return Artifact{}, false
		}
		return Artifact{Name: fmt.Sprintf("php-%s (%s)", version, kind), URL: staticPHPBase + file, Algo: download.SHA256, Checksum: sum}, true
	}
	cli, ok1 := mk("cli")
	fpm, ok2 := mk("fpm")
	return cli, fpm, ok1 && ok2
}

// OldestMacPHP is the oldest PHP version available on macOS.
func OldestMacPHP() string {
	var v []string
	for k := range staticPHPPatch {
		v = append(v, k)
	}
	sort.Strings(v)
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
