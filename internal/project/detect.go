package project

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DefaultPHP is used when nothing in the folder says otherwise.
const DefaultPHP = "8.3"

// Resolve returns the project's configuration. A hyper.json wins; a folder
// without one is configured automatically (Laragon style): <folder>.test, the
// PHP version its composer.lock needs, the public/ docroot, a database named
// after the folder. auto reports which case applied.
func Resolve(dir string) (cfg *Config, auto bool, err error) {
	path := filepath.Join(dir, "hyper.json")
	if _, statErr := os.Stat(path); statErr == nil {
		c, err := Load(path)
		return c, false, err
	}
	c, err := Auto(dir)
	return c, true, err
}

// Auto builds the configuration for a folder with no hyper.json.
func Auto(dir string) (*Config, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	name := Slug(filepath.Base(abs))
	if name == "" {
		return nil, fmt.Errorf("cannot make a site name from the folder %q: rename it using letters or digits", filepath.Base(abs))
	}
	c := &Config{
		Name: name, Domain: name + ".test", PHP: DetectPHP(abs),
		Database: Database{Engine: "mysql", Name: DBName(name)},
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

var nonSlug = regexp.MustCompile(`[^a-z0-9]+`)

// Slug turns a folder name into a host-safe name: "My Shop_2" -> "my-shop-2".
func Slug(s string) string {
	return strings.Trim(nonSlug.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// DBName turns a site name into a MySQL database name ("my-shop" -> "my_shop").
func DBName(name string) string {
	n := strings.ReplaceAll(name, "-", "_")
	if len(n) > 60 {
		n = n[:60]
	}
	return n
}

// WebRoot returns the folder nginx should serve: public/ (Laravel and most
// frameworks), else public_html/ or web/, else the project folder itself.
func WebRoot(dir string) string {
	for _, d := range []string{"public", "public_html", "web"} {
		p := filepath.Join(dir, d)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
	}
	return dir
}

// IsLaravelLike reports whether the folder looks like a Laravel app, which
// is when Hyper Stack should manage its .env.
func IsLaravelLike(dir string) bool {
	for _, f := range []string{"artisan", ".env", ".env.example"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

type ver struct{ major, minor int }

func (a ver) less(b ver) bool { return a.major < b.major || (a.major == b.major && a.minor < b.minor) }

var supportedVers = []ver{{8, 2}, {8, 3}, {8, 4}}

// DetectPHP picks the lowest supported PHP that satisfies composer.json and
// every package in composer.lock. (Only the lower bounds matter here: a
// lock file resolved on a newer PHP can require it even when composer.json
// says "^8.3".) With no composer files it returns DefaultPHP.
func DetectPHP(dir string) string {
	need, found := ver{}, false
	consider := func(constraint string) {
		if v, ok := lowerBound(constraint); ok {
			found = true
			if need.less(v) {
				need = v
			}
		}
	}

	var cj struct {
		Require map[string]string `json:"require"`
	}
	if readJSON(filepath.Join(dir, "composer.json"), &cj) {
		consider(cj.Require["php"])
	}
	var lock struct {
		Packages    []lockPkg `json:"packages"`
		PackagesDev []lockPkg `json:"packages-dev"`
	}
	if readJSON(filepath.Join(dir, "composer.lock"), &lock) {
		for _, p := range append(lock.Packages, lock.PackagesDev...) {
			consider(p.Require["php"])
		}
	}
	if !found {
		return DefaultPHP
	}
	for _, s := range supportedVers {
		if !s.less(need) {
			return fmt.Sprintf("%d.%d", s.major, s.minor)
		}
	}
	last := supportedVers[len(supportedVers)-1]
	return fmt.Sprintf("%d.%d", last.major, last.minor)
}

type lockPkg struct {
	Require map[string]string `json:"require"`
}

func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	b = bytes.TrimPrefix(b, utf8BOM)
	return json.Unmarshal(b, v) == nil
}

var verRe = regexp.MustCompile(`^v?(\d+)(?:\.(\d+|\*|x))?`)

// lowerBound returns the lowest PHP version a composer constraint accepts, as
// major.minor. Alternatives ("^8.1 || ^8.2") take the lowest; upper-bound-only
// parts ("<8.4") and wildcards impose nothing.
func lowerBound(constraint string) (ver, bool) {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		return ver{}, false
	}
	var best ver
	have := false
	for _, alt := range strings.Split(strings.ReplaceAll(constraint, "||", "|"), "|") {
		low, ok := altLower(alt)
		if !ok {
			return ver{}, false // one alternative accepts anything: no requirement
		}
		if !have || low.less(best) {
			best, have = low, true
		}
	}
	return best, have
}

func altLower(alt string) (ver, bool) {
	var best ver
	have := false
	for _, tok := range strings.FieldsFunc(alt, func(r rune) bool { return r == ' ' || r == ',' }) {
		tok = strings.TrimLeft(tok, "=!")
		if strings.HasPrefix(tok, "<") {
			continue
		}
		tok = strings.TrimLeft(tok, ">^~=v ")
		m := verRe.FindStringSubmatch(tok)
		if m == nil {
			continue
		}
		maj, _ := strconv.Atoi(m[1])
		min := 0
		if m[2] != "" && m[2] != "*" && m[2] != "x" {
			min, _ = strconv.Atoi(m[2])
		}
		v := ver{maj, min}
		if !have || best.less(v) {
			best, have = v, true
		}
	}
	return best, have
}
