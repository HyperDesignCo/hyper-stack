package doctor

import (
	"strings"
	"testing"
)

func TestParseOwner(t *testing.T) {
	o, ok := parseOwner("31276|httpd|D:\\Laragon\\laragon\\bin\\apache\\bin\\httpd.exe\r\n")
	if !ok || o.PID != 31276 || o.Name != "httpd" {
		t.Fatalf("got %+v %v", o, ok)
	}
	if _, ok := parseOwner("  "); ok {
		t.Fatal("empty output means no owner")
	}
}

func TestHintNamesLaragon(t *testing.T) {
	h := Hint(Owner{PID: 1, Name: "httpd", Path: `D:\Laragon\laragon\bin\apache\httpd.exe`}, 80)
	if !strings.Contains(h, "Laragon") || !strings.Contains(h, "80") {
		t.Fatalf("hint: %s", h)
	}
}

func TestListenPIDReadsNetstat(t *testing.T) {
	out := "  Proto  Local Address          Foreign Address        State           PID\r\n" +
		"  TCP    0.0.0.0:80             0.0.0.0:0              LISTENING       4424\r\n" +
		"  TCP    127.0.0.1:3307         0.0.0.0:0              LISTENING       24852\r\n" +
		"  TCP    127.0.0.1:41590        127.0.0.1:443          CLOSING         31020\r\n" +
		"  TCP    [::]:8443              [::]:0                 LISTENING       777\r\n"
	for port, want := range map[int]int{80: 4424, 3307: 24852, 8443: 777, 443: 0, 330: 0} {
		if got := listenPID(out, port); got != want {
			t.Errorf("port %d: got %d, want %d", port, got, want)
		}
	}
}
