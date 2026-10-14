package requests

import (
	"fmt"
	"os/exec"
	"strings"
)

// Keys live in the macOS login Keychain as generic passwords: service "crew-requests", account
// "<app>/<env>/<name>". Nothing in crew writes them to a file or prints them.

const keychainService = "crew-requests"

func account(e Env, name string) string { return e.App + "/" + e.Name + "/" + name }

// GetKey reads a key's value.
func GetKey(e Env, name string) (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", keychainService, "-a", account(e, name), "-w").Output()
	if err != nil {
		return "", fmt.Errorf("no key %s for %s %s", name, e.App, e.Name)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// SetKey stores (or replaces) a key's value.
// ponytail: the value is an argument to `security` for a moment, visible to your own user's
// processes; move to the Security framework (cgo) if that matters.
func SetKey(e Env, name, value string) error {
	out, err := exec.Command("security", "add-generic-password", "-U", "-s", keychainService, "-a", account(e, name), "-w", value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("keychain: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// HasKey reports whether a key is set, without reading its value.
func HasKey(e Env, name string) bool {
	return exec.Command("security", "find-generic-password", "-s", keychainService, "-a", account(e, name)).Run() == nil
}

// DeleteKey removes a key's value.
func DeleteKey(e Env, name string) {
	exec.Command("security", "delete-generic-password", "-s", keychainService, "-a", account(e, name)).Run()
}
