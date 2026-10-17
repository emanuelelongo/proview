package appprofile_test

import (
	"path/filepath"
	"testing"

	"emanuelelongo/proview/internal/applog"
	"emanuelelongo/proview/internal/appprofile"
	"emanuelelongo/proview/pkg/signatures"
)

func TestCustomProfileRoutesLibraryAndDiagnosticsTogether(t *testing.T) {
	previous := appprofile.Root
	appprofile.Root = t.TempDir()
	t.Cleanup(func() { appprofile.Root = previous })
	store, err := signatures.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	logs, err := applog.Directory()
	if err != nil {
		t.Fatal(err)
	}
	if store.Root != filepath.Join(appprofile.Root, "signatures") || logs != filepath.Join(appprofile.Root, "logs") {
		t.Fatalf("profile storage escaped the selected root: signatures=%s logs=%s", store.Root, logs)
	}
}
