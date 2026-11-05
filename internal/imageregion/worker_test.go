//go:build largeimageexperiment && cgo

package imageregion

import (
	"context"
	"image"
	"testing"
	"time"
)

func TestWorkerDiscardsActiveResultAndCoalescesBurst(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls int
	w := NewWorker(time.Millisecond, func(ctx context.Context, r Request) (*image.RGBA, error) {
		calls++
		if calls == 1 {
			close(started)
			<-release
			if ctx.Err() != context.Canceled {
				t.Error("active request was not canceled")
			}
		}
		// Simulate a decoder that finishes despite cancellation.
		return image.NewRGBA(image.Rect(0, 0, 1, 1)), nil
	})
	defer w.Close()
	w.Submit(Request{})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("decode did not start")
	}
	for range 10_000 {
		w.Submit(Request{})
	}
	last := w.Submit(Request{})
	close(release)
	select {
	case result := <-w.Results():
		if result.Sequence != last || !w.Current(last) {
			t.Fatalf("stale result %d, want %d", result.Sequence, last)
		}
	case <-time.After(time.Second):
		t.Fatal("latest request did not complete")
	}
	w.Close()
	<-w.Done()
	if calls != 2 {
		t.Fatalf("burst performed %d decodes, want 2", calls)
	}
	if w.Submit(Request{}) != 0 || w.Current(last) {
		t.Fatal("closed worker accepted work")
	}
}

func TestCloseDoesNotWaitForDecoderOrPublishLateResult(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	w := NewWorker(0, func(ctx context.Context, r Request) (*image.RGBA, error) { close(started); <-release; return nil, nil })
	w.Submit(Request{})
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("decode did not start")
	}
	closed := make(chan struct{})
	go func() { w.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close blocked on native decode")
	}
	close(release)
	select {
	case <-w.Done():
	case <-time.After(time.Second):
		t.Fatal("worker leaked")
	}
	if _, ok := <-w.Results(); ok {
		t.Fatal("published after close")
	}
}
