package fileassoc

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Register adds Proview to Open With for the current user. The executable stays
// portable: launching it after moving it updates the registered command path.
func Register(extensions []string) error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	root, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes`, registry.READ|registry.WRITE)
	if err != nil {
		return err
	}
	defer root.Close()
	changed, err := registerAt(root, executable, extensions)
	if changed {
		// SHCNE_ASSOCCHANGED, SHCNF_IDLIST: refresh Explorer's association cache.
		windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify").Call(0x08000000, 0, 0, 0)
	}
	return err
}

func registerAt(root registry.Key, executable string, extensions []string) (bool, error) {
	changed := false
	set := func(path, name, value string) error {
		key, _, err := registry.CreateKey(root, path, registry.READ|registry.WRITE)
		if err != nil {
			return fmt.Errorf("file associations: %s: %w", path, err)
		}
		defer key.Close()
		if current, kind, err := key.GetStringValue(name); err == nil && kind == registry.SZ && current == value {
			return nil
		}
		if err := key.SetStringValue(name, value); err != nil {
			return fmt.Errorf("file associations: %s: %w", path, err)
		}
		changed = true
		return nil
	}
	command := `"` + executable + `" "%1"`
	icon := `"` + executable + `",0`
	for _, extension := range extensions {
		progID := "com.emanuelelongo.proview" + extension
		for _, entry := range []struct{ path, name, value string }{
			{progID, "", "Proview " + strings.ToUpper(strings.TrimPrefix(extension, ".")) + " file"},
			{progID + `\DefaultIcon`, "", icon},
			{progID + `\shell\open\command`, "", command},
			{progID + `\Application`, "ApplicationName", "Proview"},
			{progID + `\Application`, "ApplicationIcon", icon},
			{extension + `\OpenWithProgids`, progID, ""},
		} {
			if err := set(entry.path, entry.name, entry.value); err != nil {
				return changed, err
			}
		}
	}
	return changed, nil
}
