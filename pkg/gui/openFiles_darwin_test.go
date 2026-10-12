//go:build darwin

package gui

import (
	"reflect"
	"testing"
	"time"
)

func TestFinderFilesReceivedDuringLaunchAreDeliveredOnceWhenReady(t *testing.T) {
	var inbox finderFileInbox
	inbox.receive([]string{"/tmp/first.jpeg"})
	inbox.receive([]string{"/tmp/second.pdf", "/tmp/third.png"})
	inbox.setCallback(nil)
	received := make(chan []string, 3)
	callback := func(files []string) { received <- files }
	inbox.setCallback(callback)
	select {
	case files := <-received:
		want := []string{"/tmp/first.jpeg", "/tmp/second.pdf", "/tmp/third.png"}
		if !reflect.DeepEqual(files, want) {
			t.Fatalf("files = %v, want %v", files, want)
		}
	case <-time.After(time.Second):
		t.Fatal("launch documents were lost")
	}
	inbox.setCallback(callback)
	inbox.receive(nil)
	inbox.receive([]string{"/tmp/later.jpeg"})
	select {
	case files := <-received:
		if !reflect.DeepEqual(files, []string{"/tmp/later.jpeg"}) {
			t.Fatalf("duplicate or unexpected files: %v", files)
		}
	case <-time.After(time.Second):
		t.Fatal("running application did not receive document")
	}
	select {
	case files := <-received:
		t.Fatalf("unexpected duplicate delivery: %v", files)
	default:
	}
}
