package openfiles

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSecondLaunchForwardsAbsolutePathsAndRestartReclaimsEndpoint(t *testing.T) {
	directory := t.TempDir()
	requests := make(chan []string, 1)
	server, forwarded, err := Start(directory, nil, func(paths []string) error { requests <- paths; return nil })
	if err != nil || forwarded || server == nil {
		t.Fatalf("primary startup: %v %v", forwarded, err)
	}
	defer server.Close()
	paths := []string{"image à 東京 #%.png", filepath.Join(t.TempDir(), "document.pdf")}
	second, forwarded, err := Start(directory, paths, func([]string) error { t.Error("secondary received request"); return nil })
	if err != nil || !forwarded || second != nil {
		t.Fatalf("secondary startup: %v %v", forwarded, err)
	}
	select {
	case got := <-requests:
		absolute, _ := filepath.Abs(paths[0])
		if !reflect.DeepEqual(got, []string{absolute, paths[1]}) {
			t.Fatalf("forwarded paths: %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request was not delivered")
	}
	empty, forwarded, err := Start(directory, nil, func([]string) error { return nil })
	if err != nil || forwarded || empty != nil {
		t.Fatalf("empty secondary: %v %v", forwarded, err)
	}
	server.Close()
	if err := os.WriteFile(filepath.Join(directory, "open-files.json"), []byte("stale endpoint"), 0600); err != nil {
		t.Fatal(err)
	}
	restarted, forwarded, err := Start(directory, nil, func([]string) error { return nil })
	if err != nil || forwarded || restarted == nil {
		t.Fatalf("restart: %v %v", forwarded, err)
	}
	restarted.Close()
}

func TestEndpointRejectsUnauthenticatedAndRelativePaths(t *testing.T) {
	directory := t.TempDir()
	requests := make(chan []string, 1)
	server, _, err := Start(directory, nil, func(paths []string) error { requests <- paths; return nil })
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
