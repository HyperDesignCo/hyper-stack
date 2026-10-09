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
