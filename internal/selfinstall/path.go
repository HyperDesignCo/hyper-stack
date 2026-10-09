// Package selfinstall puts hyper.exe in a stable folder, on the user's PATH, in
// the Start menu, and (optionally) in login autostart. Nothing here needs admin.
package selfinstall

import (
	"path/filepath"
	"strings"
)

func sameDir(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(filepath.Clean(a), `\/`), strings.TrimRight(filepath.Clean(b), `\/`))
}

// AddToPath returns pathValue with dir appended, unchanged if already present
// (case-insensitive, trailing separators ignored). Other entries stay as written.
func AddToPath(pathValue, dir string) string {
	for _, e := range strings.Split(pathValue, ";") {
		if e != "" && sameDir(e, dir) {
			return pathValue
		}
	}
	if pathValue == "" {
		return dir
	}
	return strings.TrimRight(pathValue, ";") + ";" + dir
}

// RemoveFromPath returns pathValue without dir. Other entries stay as written.
func RemoveFromPath(pathValue, dir string) string {
	var keep []string
	for _, e := range strings.Split(pathValue, ";") {
		if e == "" || sameDir(e, dir) {
			continue
		}
		keep = append(keep, e)
	}
	return strings.Join(keep, ";")
}
