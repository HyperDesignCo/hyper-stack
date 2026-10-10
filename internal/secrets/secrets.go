// Package secrets reads the MySQL root password that older Hyper Stack installs
// kept in the OS credential store (Credential Manager on Windows, Keychain on
// macOS). New installs do not create one: like Laragon and XAMPP, root has an
// empty password and MySQL only listens on 127.0.0.1.
package secrets

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const service = "HyperStack"

// lookup is the credential store read, replaceable in tests.
var lookup = func() (string, error) { return keyring.Get(service, "mysql-root") }

// MySQLRoot returns the stored root password, or ErrNotFound.
func MySQLRoot() (string, error) {
	v, err := lookup()
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("cannot read the credential store: %w", err)
	}
	return v, nil
}

// ErrNotFound means no password is stored.
var ErrNotFound = errors.New("no MySQL password stored")

// RootPassword is the password to use for MySQL root: the one an older install
// stored, or "" (no password) for installs made without one.
func RootPassword() string {
	pw, err := MySQLRoot()
	if err != nil {
		return ""
	}
	return pw
}

// Delete removes a stored password (older installs).
func Delete() error {
	err := keyring.Delete(service, "mysql-root")
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
