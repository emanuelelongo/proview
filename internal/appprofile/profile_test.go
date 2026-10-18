package appprofile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootAliasesShareProfileAndDefaultStaysCompatible(t *testing.T) {
	previous := Root
	t.Cleanup(func() { Root = previous })
	Root = ""
	if ID() != "com.emanuelelongo.proview" || CredentialService("google-drive") != "com.emanuelelongo.proview.google-drive" {
		t.Fatal("default identity changed")
	}
	root, err := Resolve(filepath.Join(t.TempDir(), "profile"))
	if err != nil {
		t.Fatal(err)
	}
	Root = root
	id := ID()
	if id == "com.emanuelelongo.proview" {
		t.Fatal("custom profile shares default identity")
	}
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, link); err != nil {
		t.Skip(err)
	}
	resolved, err := Resolve(link)
	if err != nil {
		t.Fatal(err)
	}
	Root = resolved
	if resolved != root || ID() != id {
		t.Fatal("aliases create separate profiles")
	}
	Root = filepath.Join(root, "other")
	if ID() == id {
		t.Fatal("distinct roots share credentials/preferences")
	}
}
