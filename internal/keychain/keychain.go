// Package keychain keeps secrets (REQUESTS' keys, the Jira API token) in the macOS login Keychain
// as generic passwords, by service and account. Nothing in crew writes them to a file or prints them.
package keychain

import (
	"fmt"
	"os/exec"
	"strings"
)

// Get reads a secret's value.
func Get(service, account string) (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", service, "-a", account, "-w").Output()
	if err != nil {
		return "", fmt.Errorf("nothing in the Keychain for %s", account)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// Set stores (or replaces) a secret's value.
// ponytail: the value is an argument to `security` for a moment, visible to your own user's
// processes; move to the Security framework (cgo) if that matters.
func Set(service, account, value string) error {
	out, err := exec.Command("security", "add-generic-password", "-U", "-s", service, "-a", account, "-w", value).CombinedOutput()
	if err != nil {
		return fmt.Errorf("keychain: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// Has reports whether a secret is set, without reading its value.
func Has(service, account string) bool {
	return exec.Command("security", "find-generic-password", "-s", service, "-a", account).Run() == nil
}

// Delete removes a secret.
func Delete(service, account string) {
	exec.Command("security", "delete-generic-password", "-s", service, "-a", account).Run()
}
