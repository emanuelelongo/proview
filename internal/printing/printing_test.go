package printing

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPrintFileRemainsPrivateUntilConsumedAndIsCleanedOnEveryExit(t *testing.T) {
	for _, result := range []error{nil, ErrCanceled, errors.New("printer unavailable")} {
		var path string
		err := withPrintFile([]byte("prepared PDF"), func(file string) error {
			path = file
			data, err := os.ReadFile(file)
			if err != nil || string(data) != "prepared PDF" {
				t.Fatal("prepared bytes unavailable during spooling", err)
			}
			info, err := os.Stat(file)
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
				t.Fatal("print file is not private")
			}
			return result
		})
		if !errors.Is(err, result) {
			t.Fatalf("got %v, want %v", err, result)
		}
		if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
			t.Fatal("private print directory was retained")
		}
	}
}
