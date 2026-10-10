package selfinstall

import (
	"fmt"
	"strings"
)

const (
	rcBegin = "# >>> hyper-stack >>>"
	rcEnd   = "# <<< hyper-stack <<<"
)

// AddPathBlock returns a shell startup file's content with a block that puts dir
// on PATH. An existing block is replaced, so running it twice changes nothing;
// the rest of the file is left exactly as it was.
func AddPathBlock(content, dir string) string {
	rest := RemovePathBlock(content)
	block := fmt.Sprintf("%s\nexport PATH=%s:\"$PATH\"\n%s\n", rcBegin, shDouble(dir), rcEnd)
	if rest != "" && !strings.HasSuffix(rest, "\n") {
		rest += "\n"
	}
	return rest + block
}

// RemovePathBlock returns content without the Hyper Stack block.
func RemovePathBlock(content string) string {
	lines := strings.Split(content, "\n")
	var keep []string
	skip := false
	for _, l := range lines {
		switch strings.TrimSpace(l) {
		case rcBegin:
			skip = true
			continue
		case rcEnd:
			skip = false
			continue
		}
		if !skip {
			keep = append(keep, l)
		}
	}
	out := strings.Join(keep, "\n")
	// the block was appended after a final newline: do not leave a stray blank line behind
	return strings.TrimRight(out, "\n") + trailingNewline(out)
}

func trailingNewline(s string) string {
	if s == "" || !strings.HasSuffix(s, "\n") {
		return ""
	}
	return "\n"
}

// shDouble quotes a path for a double-quoted shell string, keeping $HOME-style
// expansion off for everything except a leading home directory.
func shDouble(p string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`")
	return `"` + r.Replace(p) + `"`
}

// LaunchAgentLabel identifies the login item.
const LaunchAgentLabel = "com.hyperdesign.hyperstack"

// LaunchAgentPlist is the macOS login item that starts the tray dashboard, which
// then starts the projects flagged for autostart.
func LaunchAgentPlist(exe string) string {
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(exe)
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>` + LaunchAgentLabel + `</string>
    <key>ProgramArguments</key>
    <array>
        <string>` + esc + `</string>
        <string>ui</string>
        <string>--no-browser</string>
        <string>--startup</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
</dict>
</plist>
`
}
