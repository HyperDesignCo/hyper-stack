package secrets

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func withLookup(t *testing.T, f func() (string, error)) {
	t.Helper()
	old := lookup
	lookup = f
	t.Cleanup(func() { lookup = old })
}

func TestNewInstallsHaveNoPassword(t *testing.T) {
	withLookup(t, func() (string, error) { return "", keyring.ErrNotFound })
	if pw := RootPassword(); pw != "" {
		t.Fatalf("a new install must use an empty root password, got %q", pw)
	}
}

func TestOlderInstallKeepsItsStoredPassword(t *testing.T) {
	withLookup(t, func() (string, error) { return "stored-secret", nil })
	if pw := RootPassword(); pw != "stored-secret" {
		t.Fatalf("got %q", pw)
	}
}

func TestUnreadableCredentialStoreFallsBackToNoPassword(t *testing.T) {
	withLookup(t, func() (string, error) { return "", errors.New("store locked") })
	if pw := RootPassword(); pw != "" {
		t.Fatalf("got %q", pw)
	}
	if _, err := MySQLRoot(); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("a locked store must be reported as an error, not as 'not found': %v", err)
	}
}
