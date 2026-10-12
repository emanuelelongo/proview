//go:build !windows

package fileassoc

// macOS and Linux declare their supported types in package metadata.
func Register([]string) error { return nil }
