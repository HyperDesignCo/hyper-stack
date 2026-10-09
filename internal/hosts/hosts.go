// Package hosts edits only the Hyper Stack block of a hosts file.
package hosts

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

const (
	beginMarker = "# >>> hyper-stack >>>"
	endMarker   = "# <<< hyper-stack <<<"
)

// Render returns content with the managed block replaced by one entry per
// domain (sorted, deduplicated). Everything outside the block is untouched.
// An empty domain list removes the block.
func Render(content string, domains []string) string {
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	rest := strip(content, nl)

	set := map[string]bool{}
	for _, d := range domains {
		set[d] = true
	}
	if len(set) == 0 {
		return rest
	}
	names := make([]string, 0, len(set))
	for d := range set {
		names = append(names, d)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString(rest)
	if rest != "" && !strings.HasSuffix(rest, nl) {
		b.WriteString(nl)
	}
	b.WriteString(beginMarker + nl)
	for _, d := range names {
		b.WriteString("127.0.0.1 " + d + nl)
	}
	b.WriteString(endMarker + nl)
	return b.String()
}

// Domains returns the domains currently inside the managed block.
func Domains(content string) []string {
	var out []string
	in := false
	for _, l := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		switch strings.TrimSpace(l) {
		case beginMarker:
			in = true
		case endMarker:
			in = false
		default:
			if f := strings.Fields(l); in && len(f) >= 2 {
				out = append(out, f[1])
			}
		}
	}
	return out
}

// Update rewrites the hosts file at path with the given domains, saving the
// previous content to backup before each change.
// It returns false when nothing needed to change.
func Update(path, backup string, domains []string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("cannot read hosts file %s: %w", path, err)
	}
	out := Render(string(data), domains)
	if out == string(data) {
		return false, nil
	}
	if err := os.WriteFile(backup, data, 0o644); err != nil { //nolint:gosec // path is the OS hosts file or a test file; backup is in our data dir
		return false, fmt.Errorf("cannot back up hosts file: %w", err)
	}
	if err := os.WriteFile(path, []byte(out), 0o644); err != nil { //nolint:gosec // path is the OS hosts file or a test file
		return false, fmt.Errorf("cannot write hosts file %s: %w. Run the elevated helper (hyper install)", path, err)
	}
	return true, nil
}

func strip(content, nl string) string {
	lines := strings.Split(content, nl)
	var keep []string
	skip := false
	for _, l := range lines {
		switch strings.TrimSpace(l) {
		case beginMarker:
			skip = true
			continue
		case endMarker:
			skip = false
			continue
		}
		if !skip {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, nl)
}
