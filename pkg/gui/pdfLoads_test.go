package gui

import (
	"context"
	"errors"
	"image"
	"sync/atomic"
	"testing"
)

func TestCanceledPDFLoadsDoNotEnterNativeRenderer(t *testing.T) {
	g := &Gui{pdfPageLoading: make([]bool, 3)}
	entered, release, finished := make(chan struct{}, 2), make(chan struct{}), make(chan error, 3)
	var nativeCalls atomic.Int32
	for page := 0; page < 2; page++ {
		job, slots := g.startPDFRasterJob(page, false)
		go func() {
			_, err := job.render(slots, func() (image.Image, error) { nativeCalls.Add(1); entered <- struct{}{}; <-release; return nil, nil })
			finished <- err
		}()
	}
	<-entered
	<-entered
	job, slots := g.startPDFRasterJob(2, false)
	g.pdfPageLoading[2] = true
	go func() {
		_, err := job.render(slots, func() (image.Image, error) { nativeCalls.Add(1); return nil, nil })
		finished <- err
	}()
	g.pdfKeepStart, g.pdfKeepEnd = 10, 12
	g.cancelDistantPDFLoads()
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting load was not canceled: %v", err)
	}
	if nativeCalls.Load() != 2 || g.pdfPageLoading[2] || len(g.pdfPageJobs) != 0 {
		t.Fatal("obsolete viewport work survived cancellation")
	}
	close(release)
	<-finished
	<-finished
}
