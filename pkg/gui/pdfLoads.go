package gui

import (
	"context"
	"image"
)

// Bound native page work even when the scrollbar skips hundreds of pages.
// Cancellation drops waiting requests; at most two obsolete native calls may
// finish because the native renderer itself is not interruptible.
type pdfRasterJob struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (g *Gui) startPDFRasterJob(page int, thumbnail bool) (*pdfRasterJob, chan struct{}) {
	if g.pdfLoadSlots == nil {
		g.pdfLoadSlots = make(chan struct{}, 2)
	}
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
	return job, g.pdfLoadSlots
}

func (job *pdfRasterJob) render(slots chan struct{}, render func() (image.Image, error)) (image.Image, error) {
	defer job.cancel()
	select {
	case slots <- struct{}{}:
		defer func() { <-slots }()
	case <-job.ctx.Done():
		return nil, job.ctx.Err()
	}
	if err := job.ctx.Err(); err != nil {
		return nil, err
	}
	return render()
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
	for _, job := range g.pdfPageJobs {
		job.cancel()
	}
	for _, job := range g.pdfThumbJobs {
		job.cancel()
	}
	g.pdfPageJobs, g.pdfThumbJobs = nil, nil
}
