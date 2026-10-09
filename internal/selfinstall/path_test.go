package selfinstall

import "testing"

func TestAddToPath(t *testing.T) {
	const dir = `C:\Users\Me\AppData\Local\HyperStack\app`
	cases := []struct{ name, in, want string }{
		{"empty", "", dir},
		{"append", `C:\a;C:\b`, `C:\a;C:\b;` + dir},
		{"trailing semicolon", `C:\a;`, `C:\a;` + dir},
		{"already present", `C:\a;` + dir, `C:\a;` + dir},
		{"present, different case and slash", `C:\a;c:\users\me\appdata\local\hyperstack\app\`, `C:\a;c:\users\me\appdata\local\hyperstack\app\`},
		{"keeps %VARS% untouched", `%USERPROFILE%\bin;C:\a`, `%USERPROFILE%\bin;C:\a;` + dir},
	}
	for _, c := range cases {
		if got := AddToPath(c.in, dir); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestRemoveFromPath(t *testing.T) {
	const dir = `C:\h\app`
	if got := RemoveFromPath(`C:\a;`+dir+`;C:\b;`, dir); got != `C:\a;C:\b` {
		t.Errorf("got %q", got)
	}
	if got := RemoveFromPath(`C:\a`, dir); got != `C:\a` {
		t.Errorf("unrelated PATH changed: %q", got)
	}
	once := AddToPath(`C:\a`, dir)
	if RemoveFromPath(once, dir) != `C:\a` {
		t.Error("add then remove must round-trip")
	}
}
