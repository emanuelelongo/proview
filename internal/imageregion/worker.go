//go:build largeimageexperiment && cgo

package imageregion

import (
	"context"
	"image"
	"sync"
	"time"
)

type Result struct {
	Sequence uint64
	Request  Request
	Image    *image.RGBA
	Err      error
}

// Worker retains at most one active decode, one pending request and one result.
// Submit never waits for decoding or delivery. Read Results off the UI thread;
// recheck Current on the UI thread immediately before installing a result.
type Worker struct {
	mu       sync.Mutex
	sequence uint64
	pending  Request
	closed   bool
	cancel   context.CancelFunc
	wake     chan struct{}
	stop     chan struct{}
	done     chan struct{}
	results  chan Result
	decode   func(context.Context, Request) (*image.RGBA, error)
	settle   time.Duration
}

func NewWorker(settle time.Duration, decode func(context.Context, Request) (*image.RGBA, error)) *Worker {
	w := &Worker{wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), results: make(chan Result, 1), decode: decode, settle: settle}
	go w.run()
	return w
}

func (w *Worker) Submit(r Request) uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return 0
	}
	w.sequence++
	w.pending = r
	if w.cancel != nil {
		w.cancel()
	}
	// Drop a completed frame as soon as a newer viewport exists.
	select {
	case <-w.results:
	default:
	}
	select {
	case w.wake <- struct{}{}:
	default:
	}
	return w.sequence
}

func (w *Worker) Results() <-chan Result { return w.results }
func (w *Worker) Current(sequence uint64) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return !w.closed && sequence == w.sequence
}

// Close only signals cancellation; teardown never waits for native code on UI.
func (w *Worker) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	if w.cancel != nil {
		w.cancel()
	}
	select {
	case <-w.results:
	default:
	}
	close(w.stop)
}
func (w *Worker) Done() <-chan struct{} { return w.done }

func (w *Worker) run() {
	defer close(w.done)
	defer close(w.results)
	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()
	for {
		select {
		case <-w.stop:
			return
		case <-w.wake:
		}
		// Every motion restarts a single timer; no timer/goroutine per event.
		timer.Reset(w.settle)
	settling:
		for {
			select {
			case <-w.stop:
				return
			case <-w.wake:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(w.settle)
			case <-timer.C:
				break settling
			}
		}
		w.mu.Lock()
		if w.closed {
			w.mu.Unlock()
			return
		}
		request, sequence := w.pending, w.sequence
		// Consume notifications already represented by this snapshot.
		select {
		case <-w.wake:
		default:
		}
		ctx, cancel := context.WithCancel(context.Background())
		w.cancel = cancel
		w.mu.Unlock()
		pixels, err := w.decode(ctx, request)
		cancel()
		w.mu.Lock()
		w.cancel = nil
		if !w.closed && sequence == w.sequence {
			select {
			case <-w.results:
			default:
			}
			w.results <- Result{Sequence: sequence, Request: request, Image: pixels, Err: err}
		}
		w.mu.Unlock()
	}
}
