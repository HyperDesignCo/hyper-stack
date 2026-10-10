// Package certs creates a local CA and leaf certificates for *.test domains.
// It only writes files; trusting the CA in the OS store is done separately by
// the elevated helper during hyper install.
package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const (
	caCertName = "rootCA.pem"
	caKeyName  = "rootCA-key.pem"
)

// EnsureCA creates the CA in dir if missing and returns the cert path.
func EnsureCA(dir string) (string, error) {
	certPath := filepath.Join(dir, caCertName)
	keyPath := filepath.Join(dir, caKeyName)
	if exists(certPath) && exists(keyPath) {
		return certPath, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{Organization: []string{"Hyper Stack"}, CommonName: "Hyper Stack Local CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		// The CA may only ever vouch for *.test names. Without this, a stolen CA
		// key could mint a trusted certificate for any website on this machine.
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         []string{"test"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", err
	}
	if err := writePEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", err
	}
	return certPath, writePEM(keyPath, "EC PRIVATE KEY", keyDER, 0o600)
}

// EnsureLeaf issues dir/<domain>.pem and -key.pem signed by the CA in caDir,
// unless a valid one (more than 30 days left) already exists.
func EnsureLeaf(caDir, dir, domain string) (certPath, keyPath string, err error) {
	certPath = filepath.Join(dir, domain+".pem")
	keyPath = filepath.Join(dir, domain+"-key.pem")
	if valid(certPath, domain) && exists(keyPath) {
		return certPath, keyPath, nil
	}
	ca, caKey, err := loadCA(caDir)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{Organization: []string{"Hyper Stack"}, CommonName: domain},
		DNSNames:     []string{domain, "*." + domain},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return "", "", err
	}
	if err := writePEM(certPath, "CERTIFICATE", der, 0o644); err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return certPath, keyPath, writePEM(keyPath, "EC PRIVATE KEY", keyDER, 0o600)
}

func loadCA(dir string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cb, err := os.ReadFile(filepath.Join(dir, caCertName))
	if err != nil {
		return nil, nil, fmt.Errorf("local CA missing in %s: run hyper install", dir)
	}
	kb, err := os.ReadFile(filepath.Join(dir, caKeyName))
	if err != nil {
		return nil, nil, fmt.Errorf("local CA key missing in %s: run hyper install", dir)
	}
	cp, _ := pem.Decode(cb)
	kp, _ := pem.Decode(kb)
	if cp == nil || kp == nil {
		return nil, nil, fmt.Errorf("local CA files in %s are corrupt: delete them and run hyper install", dir)
	}
	cert, err := x509.ParseCertificate(cp.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, err := x509.ParseECPrivateKey(kp.Bytes)
	return cert, key, err
}

func valid(certPath, domain string) bool {
	b, err := os.ReadFile(certPath)
	if err != nil {
		return false
	}
	p, _ := pem.Decode(b)
	if p == nil {
		return false
	}
	c, err := x509.ParseCertificate(p.Bytes)
	if err != nil {
		return false
	}
	return time.Until(c.NotAfter) > 30*24*time.Hour && c.VerifyHostname(domain) == nil
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	return n
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func writePEM(path, typ string, der []byte, mode os.FileMode) error {
	return os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), mode)
}
