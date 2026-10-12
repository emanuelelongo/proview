package fileassoc

import (
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func removeTestKey(t *testing.T, parent registry.Key, path string) {
	t.Helper()
	key, err := registry.OpenKey(parent, path, registry.READ|registry.WRITE)
	if err != nil {
		t.Errorf("open test key for cleanup: %v", err)
		return
	}
	children, err := key.ReadSubKeyNames(-1)
	if err != nil {
		key.Close()
		t.Errorf("read test keys for cleanup: %v", err)
		return
	}
	for _, child := range children {
		removeTestKey(t, key, child)
	}
	key.Close()
	if err := registry.DeleteKey(parent, path); err != nil {
		t.Errorf("delete test key: %v", err)
	}
}

func TestRegistrationPreservesDefaultsAndUpdatesMovedExecutable(t *testing.T) {
	path := fmt.Sprintf(`Software\ProviewFileAssociationTest%d`, time.Now().UnixNano())
	root, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.READ|registry.WRITE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close(); removeTestKey(t, registry.CURRENT_USER, path) })
	stl, _, err := registry.CreateKey(root, `.stl`, registry.READ|registry.WRITE)
	if err != nil {
		t.Fatal(err)
	}
	defer stl.Close()
	if err := stl.SetStringValue("", "ExistingViewer.STL"); err != nil {
		t.Fatal(err)
	}
	alternatives, _, err := registry.CreateKey(stl, `OpenWithProgids`, registry.READ|registry.WRITE)
	if err != nil {
		t.Fatal(err)
	}
	defer alternatives.Close()
	if err := alternatives.SetStringValue("ExistingViewer.STL", ""); err != nil {
		t.Fatal(err)
	}
	extensions := []string{".pdf", ".png", ".stl"}
	executable := `C:\Portable Apps\Proview\proview.exe`
	changed, err := registerAt(root, executable, extensions)
	if err != nil || !changed {
		t.Fatalf("first registration: changed=%v, error=%v", changed, err)
	}
	changed, err = registerAt(root, executable, extensions)
	if err != nil || changed {
		t.Fatalf("repeated registration should be unchanged: changed=%v, error=%v", changed, err)
	}
	moved := `D:\Another Folder\renamed.exe`
	changed, err = registerAt(root, moved, extensions)
	if err != nil || !changed {
		t.Fatalf("moved executable was not updated: changed=%v, error=%v", changed, err)
	}
	for _, extension := range extensions {
		progID := "com.emanuelelongo.proview" + extension
		command, err := registry.OpenKey(root, progID+`\shell\open\command`, registry.READ)
		if err != nil {
			t.Fatal(err)
		}
		value, _, err := command.GetStringValue("")
		command.Close()
		if err != nil || value != `"`+moved+`" "%1"` {
			t.Fatalf("command path/argument quoting = %q, error=%v", value, err)
		}
		openWith, err := registry.OpenKey(root, extension+`\OpenWithProgids`, registry.READ)
		if err != nil {
			t.Fatal(err)
		}
		value, _, err = openWith.GetStringValue(progID)
		openWith.Close()
		if err != nil || value != "" {
			t.Fatalf("missing Open With entry for %s: %v", extension, err)
		}
	}
	if value, _, err := stl.GetStringValue(""); err != nil || value != "ExistingViewer.STL" {
		t.Fatal("registration changed the existing file association")
	}
	if _, _, err := alternatives.GetStringValue("ExistingViewer.STL"); err != nil {
		t.Fatal("registration removed another viewer")
	}
}
