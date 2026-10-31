package gui

import (
	"context"
	"image"
)

const pdfNativeRenderSlots = 2

// Bound native page work even when the scrollbar skips hundreds of pages.
// Cancellation drops waiting requests; at most two obsolete native calls may
// finish because the native renderer itself is not interruptible.
type pdfRasterJob struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (g *Gui) startPDFRasterJob(page int, thumbnail bool) (*pdfRasterJob, chan struct{}) {
	slots := g.pdfRenderSlots()
	ctx, cancel := context.WithCancel(context.Background())
	job := &pdfRasterJob{ctx: ctx, cancel: cancel}
	if thumbnail {
		if g.pdfThumbJobs == nil {
			g.pdfThumbJobs = map[int]*pdfRasterJob{}
		}
		g.pdfThumbJobs[page] = job
	} else {
		if g.pdfPageJobs == nil {
			g.pdfPageJobs = map[int]*pdfRasterJob{}
		}
		g.pdfPageJobs[page] = job
	}
	return job, slots
}

func (job *pdfRasterJob) render(slots chan struct{}, render func() (image.Image, error)) (image.Image, error) {
	defer job.cancel()
	var picture image.Image
	err := withPDFRenderSlot(job.ctx, slots, func() error {
		var err error
		picture, err = render()
		return err
	})
	return picture, err
}

func (g *Gui) pdfRenderSlots() chan struct{} {
	g.pdfLoadSlotsOnce.Do(func() {
		if g.pdfLoadSlots == nil {
			g.pdfLoadSlots = make(chan struct{}, pdfNativeRenderSlots)
		}
	})
	return g.pdfLoadSlots
}

func withPDFRenderSlot(ctx context.Context, slots chan struct{}, render func() error) error {
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return render()
}

func (g *Gui) renderPDFBackground(ctx context.Context, render func() error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return withPDFRenderSlot(ctx, g.pdfRenderSlots(), render)
}

func (g *Gui) cancelDistantPDFLoads() {
	for page, job := range g.pdfPageJobs {
		if !g.pdfPageWanted(page) {
			job.cancel()
			delete(g.pdfPageJobs, page)
			g.pdfPageLoading[page] = false
		}
	}
	for page, job := range g.pdfThumbJobs {
		if !g.pdfThumbWanted(page) {
			job.cancel()
			delete(g.pdfThumbJobs, page)
			g.pdfThumbLoading[page] = false
		}
	}
}

func (g *Gui) clearPDFLoads() {
	g.clearSidebarThumbnailIndex()
	for _, job := range g.pdfPageJobs {
		job.cancel()
	}
	for _, job := range g.pdfThumbJobs {
		job.cancel()
	}
	g.pdfPageJobs, g.pdfThumbJobs = nil, nil
}
