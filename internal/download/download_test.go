package download

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func zipBytes(t *testing.T, files map[string]string) []byte {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for n, c := range files {
		f, _ := w.Create(n)
		f.Write([]byte(c))
	}
	w.Close()
	return b.Bytes()
}

func TestFileVerifiesChecksumAndReuses(t *testing.T) {
	body := []byte("hello")
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++; w.Write(body) }))
	defer srv.Close()
	sum := sha256.Sum256(body)
	dest := filepath.Join(t.TempDir(), "a.bin")

	if err := File(context.Background(), srv.URL, dest, SHA256, hex.EncodeToString(sum[:]), nil); err != nil {
		t.Fatal(err)
	}
	if err := File(context.Background(), srv.URL, dest, SHA256, hex.EncodeToString(sum[:]), nil); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("expected reuse, got %d downloads", hits)
	}
}

func TestFileRejectsBadChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("x")) }))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "a.bin")
	if err := File(context.Background(), srv.URL, dest, SHA256, "deadbeef", nil); err == nil {
		t.Fatal("expected mismatch error")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("bad file must not be left in place")
	}
}

func TestUnzipStripAndTraversal(t *testing.T) {
	d := t.TempDir()
	z := filepath.Join(d, "ok.zip")
	os.WriteFile(z, zipBytes(t, map[string]string{"top/bin/php.exe": "x"}), 0o644)
	out := filepath.Join(d, "out")
	if err := Unpack(z, out, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "bin", "php.exe")); err != nil {
		t.Fatal(err)
	}

	bad := filepath.Join(d, "bad.zip")
	os.WriteFile(bad, zipBytes(t, map[string]string{"top/../../evil.txt": "x"}), 0o644)
	if err := Unpack(bad, filepath.Join(d, "out2"), true); err == nil {
		t.Fatal("expected traversal to be rejected")
	}
	if _, err := os.Stat(filepath.Join(d, "evil.txt")); err == nil {
		t.Fatal("file escaped destination")
	}
}

func TestUntarGz(t *testing.T) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "mysql/bin/mysqld", Mode: 0o755, Size: 1, Typeflag: tar.TypeReg})
	tw.Write([]byte("x"))
	tw.Close()
	gz.Close()
	d := t.TempDir()
	a := filepath.Join(d, "m.tar.gz")
	os.WriteFile(a, b.Bytes(), 0o644)
	if err := Unpack(a, filepath.Join(d, "o"), true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "o", "bin", "mysqld")); err != nil {
		t.Fatal(err)
	}
}

func TestFileVerifiesSHA512(t *testing.T) {
	body := []byte("hello")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer srv.Close()
	sum := sha512.Sum512(body)
	dest := filepath.Join(t.TempDir(), "a.bin")
	if err := File(context.Background(), srv.URL, dest, SHA512, hex.EncodeToString(sum[:]), nil); err != nil {
		t.Fatal(err)
	}
}
