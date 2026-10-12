// Package modelgpu provides an isolated GPU renderer with a software fallback.
package modelgpu

import (
	"context"
	"emanuelelongo/proview/internal/model3d"
	"image"
	"log"
	"os"
	"runtime"
	"sync"
)

type device interface {
	render(context.Context, *model3d.Mesh, model3d.Camera, int, int) (*image.RGBA, error)
	close()
	name() string
}
type request struct {
	ctx    context.Context
	mesh   *model3d.Mesh
	camera model3d.Camera
	w, h   int
	fast   bool
	result chan result
}
type result struct {
	frame *image.RGBA
	err   error
}

// Renderer owns its context and all GL resources on one locked OS thread.
type Renderer struct {
	requests chan request
	stop     chan struct{}
	done     chan struct{}
	once     sync.Once
	mu       sync.RWMutex
	backend  string
}

func NewRenderer() *Renderer {
	r := &Renderer{requests: make(chan request), stop: make(chan struct{}), done: make(chan struct{}), backend: "Initializing"}
	go r.run()
	return r
}
func (r *Renderer) Backend() string     { r.mu.RLock(); defer r.mu.RUnlock(); return r.backend }
func (r *Renderer) setBackend(s string) { r.mu.Lock(); r.backend = s; r.mu.Unlock() }
func (r *Renderer) Close()              { r.once.Do(func() { close(r.stop) }) }
func (r *Renderer) Render(ctx context.Context, mesh *model3d.Mesh, camera model3d.Camera, w, h int) (*image.RGBA, error) {
	return r.render(ctx, mesh, camera, w, h, false)
}
func (r *Renderer) RenderInteractive(ctx context.Context, mesh *model3d.Mesh, camera model3d.Camera, w, h int) (*image.RGBA, error) {
	return r.render(ctx, mesh, camera, w, h, true)
}
func (r *Renderer) render(ctx context.Context, mesh *model3d.Mesh, camera model3d.Camera, w, h int, fast bool) (*image.RGBA, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validate(mesh, camera, w, h); err != nil {
		return nil, err
	}
	req := request{fast: fast, ctx: ctx, mesh: mesh, camera: camera, w: w, h: h, result: make(chan result, 1)}
	select {
	case r.requests <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.stop:
		return nil, context.Canceled
	}
	select {
	case res := <-req.result:
		return res.frame, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.stop:
		return nil, context.Canceled
	}
}
func (r *Renderer) run() {
	defer close(r.done)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var gpu device
	if os.Getenv("PROVIEW_STL_RENDERER") != "cpu" {
		var err error
		gpu, err = newDevice()
		if err != nil {
			log.Printf("STL GPU unavailable, using CPU: %v", err)
		}
	}
	if gpu != nil {
		r.setBackend(gpu.name())
		log.Printf("STL renderer: %s", gpu.name())
		defer func() {
			if gpu != nil {
				gpu.close()
			}
		}()
	} else {
		r.setBackend("CPU")
	}
	for {
		select {
		case <-r.stop:
			return
		case req := <-r.requests:
			if req.ctx.Err() != nil {
				req.result <- result{err: req.ctx.Err()}
				continue
			}
			var frame *image.RGBA
			var err error
			if gpu != nil {
				frame, err = gpu.render(req.ctx, req.mesh, req.camera, req.w, req.h)
				if err != nil && req.ctx.Err() == nil {
					log.Printf("STL GPU render failed, using CPU: %v", err)
					gpu.close()
					gpu = nil
					r.setBackend("CPU")
				}
			}
			if gpu == nil {
				if req.fast {
					frame, err = model3d.Render(req.ctx, req.mesh, req.camera, req.w, req.h)
				} else {
					frame, err = model3d.RenderHighQuality(req.ctx, req.mesh, req.camera, req.w, req.h)
				}
			}
			req.result <- result{frame, err}
		}
	}
}
