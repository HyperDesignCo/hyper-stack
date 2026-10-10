package selfinstall

import (
	"strings"
	"testing"
)

func TestAddPathBlockIsIdempotentAndKeepsTheRest(t *testing.T) {
	const dir = "/Users/me/Library/Application Support/HyperStack/app"
	orig := "export EDITOR=vim\nalias ll='ls -l'\n"
	once := AddPathBlock(orig, dir)
	if !strings.HasPrefix(once, orig) {
		t.Fatalf("the user's own lines must come first and stay unchanged:\n%s", once)
	}
	if !strings.Contains(once, `export PATH="/Users/me/Library/Application Support/HyperStack/app":"$PATH"`) {
		t.Errorf("the PATH line is wrong (spaces must stay inside the quotes):\n%s", once)
	}
	if twice := AddPathBlock(once, dir); twice != once {
		t.Errorf("running it twice must change nothing:\n%s\n---\n%s", once, twice)
	}
	// a changed folder replaces the block, it does not add a second one
	moved := AddPathBlock(once, "/opt/hyper")
	if strings.Count(moved, rcBegin) != 1 || strings.Contains(moved, "Application Support") || !strings.Contains(moved, `"/opt/hyper"`) {
		t.Errorf("the block must be replaced:\n%s", moved)
	}
}

func TestRemovePathBlockRestoresTheFile(t *testing.T) {
	orig := "export EDITOR=vim\n\nalias ll='ls -l'\n"
	if got := RemovePathBlock(AddPathBlock(orig, "/x")); got != orig {
		t.Errorf("add then remove must give back the original:\n%q\n%q", got, orig)
	}
	if got := RemovePathBlock(orig); got != orig {
		t.Errorf("a file without the block is unchanged: %q", got)
	}
	if got := RemovePathBlock(AddPathBlock("", "/x")); got != "" {
		t.Errorf("an empty file stays empty: %q", got)
	}
}

func TestPathBlockEscapesShellCharacters(t *testing.T) {
	got := AddPathBlock("", `/Users/a "b"/$HOME/`+"`x`")
	if !strings.Contains(got, `\"b\"`) || !strings.Contains(got, `\$HOME`) || !strings.Contains(got, "\\`x\\`") {
		t.Errorf("quotes, $ and backticks must be escaped:\n%s", got)
	}
}

func TestLaunchAgentPlist(t *testing.T) {
	p := LaunchAgentPlist("/Users/me/Library/Application Support/HyperStack/app/hyper & co")
	for _, want := range []string{
		"<string>com.hyperdesign.hyperstack</string>",
		"<string>ui</string>", "<string>--no-browser</string>", "<string>--startup</string>",
		"<key>RunAtLoad</key>", "hyper &amp; co",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("missing %q in:\n%s", want, p)
		}
	}
	if strings.Contains(p, "hyper & co") {
		t.Error("the path must be XML-escaped")
	}
}
