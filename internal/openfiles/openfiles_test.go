package openfiles

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSecondLaunchForwardsAbsolutePathsAndRestartReclaimsEndpoint(t *testing.T) {
	directory := t.TempDir()
	requests := make(chan Request, 1)
	server, forwarded, err := Start(directory, Request{}, func(request Request) error { requests <- request; return nil })
	if err != nil || forwarded || server == nil {
		t.Fatalf("primary startup: %v %v", forwarded, err)
	}
	defer server.Close()
	paths := []string{"image à 東京 #%.png", filepath.Join(t.TempDir(), "document.pdf")}
	second, forwarded, err := Start(directory, Request{Paths: paths}, func(Request) error { t.Error("secondary received request"); return nil })
	if err != nil || !forwarded || second != nil {
		t.Fatalf("secondary startup: %v %v", forwarded, err)
	}
	select {
	case got := <-requests:
		absolute, _ := filepath.Abs(paths[0])
		if !reflect.DeepEqual(got.Paths, []string{absolute, paths[1]}) {
			t.Fatalf("forwarded paths: %q", got.Paths)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request was not delivered")
	}
	empty, forwarded, err := Start(directory, Request{}, func(Request) error { return nil })
	if err != nil || forwarded || empty != nil {
		t.Fatalf("empty secondary: %v %v", forwarded, err)
	}
	server.Close()
	if err := os.WriteFile(filepath.Join(directory, "open-files.json"), []byte("stale endpoint"), 0600); err != nil {
		t.Fatal(err)
	}
	restarted, forwarded, err := Start(directory, Request{}, func(Request) error { return nil })
	if err != nil || forwarded || restarted == nil {
		t.Fatalf("restart: %v %v", forwarded, err)
	}
	restarted.Close()
}

func TestEndpointRejectsUnauthenticatedAndRelativePaths(t *testing.T) {
	directory := t.TempDir()
	requests := make(chan Request, 1)
	server, _, err := Start(directory, Request{}, func(request Request) error { requests <- request; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	data, err := os.ReadFile(filepath.Join(directory, "open-files.json"))
	if err != nil {
		t.Fatal(err)
	}
	var info endpoint
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	for _, input := range []request{{Token: "wrong", Paths: []string{filepath.Join(directory, "doc.pdf")}}, {Token: info.Token, Paths: []string{"relative.pdf"}}} {
		connection, err := net.Dial("tcp4", info.Address)
		if err != nil {
			t.Fatal(err)
		}
		_ = connection.SetDeadline(time.Now().Add(time.Second))
		if err := json.NewEncoder(connection).Encode(input); err != nil {
			t.Fatal(err)
		}
		reply := make([]byte, 3)
		n, _ := connection.Read(reply)
		_ = connection.Close()
		if n > 0 {
			t.Fatalf("invalid request accepted: %q", reply[:n])
		}
	}
	select {
	case <-requests:
		t.Fatal("invalid request was delivered")
	default:
	}
}

func TestExplicitLaunchOptionsForwardWithoutFiles(t *testing.T) {
	directory := t.TempDir()
	requests := make(chan Request, 1)
	server, _, err := Start(directory, Request{}, func(request Request) error { requests <- request; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	for _, input := range []Request{{NewWindow: true}, {WorkDir: directory}, {NewWindow: true, WorkDir: directory, Paths: []string{"photo.png"}}} {
		secondary, forwarded, err := Start(directory, input, func(Request) error { t.Error("secondary received request"); return nil })
		if err != nil || !forwarded || secondary != nil {
			t.Fatal("options were not forwarded", forwarded, err)
		}
		select {
		case received := <-requests:
			if received.NewWindow != input.NewWindow || received.WorkDir != input.WorkDir || len(received.Paths) != len(input.Paths) {
				t.Fatal("launch options lost", received)
			}
			if len(input.Paths) > 0 {
				absolute, _ := filepath.Abs(input.Paths[0])
				if received.Paths[0] != absolute {
					t.Fatal("file resolved against work directory instead of caller cwd")
				}
			}
		case <-time.After(3 * time.Second):
			t.Fatal("request missing")
		}
	}
}

func TestLaunchOptionsDoNotSilentlyReachAnOlderServer(t *testing.T) {
	directory := t.TempDir()
	requests := make(chan Request, 1)
	server, _, err := Start(directory, Request{}, func(request Request) error { requests <- request; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	path := filepath.Join(directory, "open-files.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var info endpoint
	if err = json.Unmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	info.Version = 0
	data, _ = json.Marshal(info)
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	secondary, forwarded, err := Start(directory, Request{NewWindow: true, Paths: []string{"photo.png"}}, func(Request) error { return nil })
	if !errors.Is(err, errLaunchOptionsUnsupported) || forwarded || secondary != nil {
		t.Fatal("old server silently accepted window options", forwarded, err)
	}
	select {
	case <-requests:
		t.Fatal("incompatible request opened files in the old window")
	default:
	}
	// Legacy path-only requests still use the compatible wire format.
	if _, forwarded, err = Start(directory, Request{Paths: []string{"photo.png"}}, func(Request) error { return nil }); err != nil || !forwarded {
		t.Fatal("legacy path forwarding broke", err)
	}
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("legacy request missing")
	}
}

func TestEndpointRejectsInvalidWorkDirectories(t *testing.T) {
	directory := t.TempDir()
	requests := make(chan Request, 1)
	server, _, err := Start(directory, Request{}, func(request Request) error { requests <- request; return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	data, err := os.ReadFile(filepath.Join(directory, "open-files.json"))
	if err != nil {
		t.Fatal(err)
	}
	var info endpoint
	if err = json.Unmarshal(data, &info); err != nil {
		t.Fatal(err)
	}
	for _, workDir := range []string{"relative", filepath.Join(directory, "missing"), filepath.Join(directory, "open-files.json"), directory + "\x00bad"} {
		connection, err := net.Dial("tcp4", info.Address)
		if err != nil {
			t.Fatal(err)
		}
		_ = connection.SetDeadline(time.Now().Add(time.Second))
		if err = json.NewEncoder(connection).Encode(request{Token: info.Token, NewWindow: true, WorkDir: workDir}); err != nil {
			t.Fatal(err)
		}
		reply := make([]byte, 3)
		n, _ := connection.Read(reply)
		_ = connection.Close()
		if n > 0 {
			t.Fatal("invalid work directory accepted", workDir)
		}
	}
	select {
	case <-requests:
		t.Fatal("invalid request delivered")
	default:
	}
}
