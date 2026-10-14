package requests

import (
	"fmt"

	"crew/internal/keychain"
)

// Keys live in the macOS login Keychain (the keychain package): service "crew-requests", account
// "<app>/<env>/<name>".

const keychainService = "crew-requests"

func account(e Env, name string) string { return e.App + "/" + e.Name + "/" + name }

// GetKey reads a key's value.
func GetKey(e Env, name string) (string, error) {
	v, err := keychain.Get(keychainService, account(e, name))
	if err != nil {
		return "", fmt.Errorf("no key %s for %s %s", name, e.App, e.Name)
	}
	return v, nil
}

// SetKey stores (or replaces) a key's value.
func SetKey(e Env, name, value string) error {
	return keychain.Set(keychainService, account(e, name), value)
}

// HasKey reports whether a key is set, without reading its value.
func HasKey(e Env, name string) bool { return keychain.Has(keychainService, account(e, name)) }

// DeleteKey removes a key's value.
func DeleteKey(e Env, name string) { keychain.Delete(keychainService, account(e, name)) }
