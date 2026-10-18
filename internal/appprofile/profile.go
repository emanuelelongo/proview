// Package appprofile selects process-wide application storage before startup.
package appprofile

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// Root is set once by main, before logging, IPC or GUI workers start.
// An empty root preserves the installed application's existing locations.
var Root string

// Resolve creates a private root and canonicalizes symlinks so aliases share IPC.
func Resolve(directory string) (string, error) {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(absolute, 0700); err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
}

// ID namespaces framework preferences and OS keychain entries by canonical root.
// Fyne retains ownership of its platform-specific preferences location.
func ID() string {
	const base = "com.emanuelelongo.proview"
	if Root == "" {
		return base
	}
	digest := sha256.Sum256([]byte(Root))
	return fmt.Sprintf("%s.profile.%x", base, digest[:16])
}

// CredentialService keeps the existing default keychain names compatible.
func CredentialService(provider string) string { return ID() + "." + provider }
