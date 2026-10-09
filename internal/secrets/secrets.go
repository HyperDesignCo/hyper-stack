// Package secrets stores the local MySQL password in the OS credential store
// (Credential Manager on Windows, Keychain on macOS), never in a text file.
package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const service = "HyperStack"

// MySQLRoot returns the stored root password, or ErrNotFound.
func MySQLRoot() (string, error) {
	v, err := keyring.Get(service, "mysql-root")
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("cannot read the credential store: %w", err)
	}
	return v, nil
}

// ErrNotFound means no password has been generated yet.
var ErrNotFound = errors.New("no MySQL password stored: run hyper install")

// NewMySQLRoot generates and stores a random password.
func NewMySQLRoot() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	pw := hex.EncodeToString(b)
	if err := keyring.Set(service, "mysql-root", pw); err != nil {
		return "", fmt.Errorf("cannot write to the credential store: %w", err)
	}
	return pw, nil
}

// Delete removes the stored password.
func Delete() error {
	err := keyring.Delete(service, "mysql-root")
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
