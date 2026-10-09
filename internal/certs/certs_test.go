package certs

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestLeafChainsToCAAndIsIdempotent(t *testing.T) {
	ca := t.TempDir()
	out := t.TempDir()
	caPath, err := EnsureCA(ca)
	if err != nil {
		t.Fatal(err)
	}
	cert, key, err := EnsureLeaf(ca, out, "bazooka.test")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(cert)

	caPEM, _ := os.ReadFile(caPath)
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	p, _ := pem.Decode(before)
	leaf, err := x509.ParseCertificate(p.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{DNSName: "bazooka.test", Roots: pool}); err != nil {
		t.Fatalf("leaf does not verify: %v", err)
	}

	if _, _, err := EnsureLeaf(ca, out, "bazooka.test"); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.ReadFile(cert); string(after) != string(before) {
		t.Fatal("certificate was re-issued")
	}
	if _, err := os.Stat(filepath.Clean(key)); err != nil {
		t.Fatal(err)
	}
}

func TestLeafWithoutCAFailsClearly(t *testing.T) {
	if _, _, err := EnsureLeaf(t.TempDir(), t.TempDir(), "x.test"); err == nil {
		t.Fatal("expected error")
	}
}
