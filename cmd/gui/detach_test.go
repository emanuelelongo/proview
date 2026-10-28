package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDetachedProcessSurvivesLauncher(t *testing.T) {
	path := filepath.Join(t.TempDir(), "child.json")
	command := exec.Command(os.Args[0], "-test.run=^TestDetachedProcessHelper$")
	command.Env = append(os.Environ(), "PROVIEW_DETACH_TEST="+path, "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8")
	if output, err := command.CombinedOutput(); err != nil || len(output) != 0 {
		t.Fatalf("launcher: %v, output: %q", err, output)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		raw, err := os.ReadFile(path)
		if err == nil {
			var result struct{ Marker, Directory string }
			if json.Unmarshal(raw, &result) == nil {
				cwd, _ := os.Getwd()
				if result.Marker != "1" || result.Directory != cwd {
					t.Fatalf("detached child = %+v", result)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("detached child did not complete after launcher exited")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDetachedProcessHelper(t *testing.T) {
	path := os.Getenv("PROVIEW_DETACH_TEST")
	if path == "" {
		return
	}
	if os.Getenv("PROVIEW_DETACHED_CHILD") != "1" {
		if err := launchDetached([]string{"-test.run=^TestDetachedProcessHelper$"}); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	time.Sleep(100 * time.Millisecond)
	cwd, _ := os.Getwd()
	raw, _ := json.Marshal(struct{ Marker, Directory string }{os.Getenv("PROVIEW_DETACHED_CHILD"), cwd})
	if os.WriteFile(path, raw, 0600) != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
