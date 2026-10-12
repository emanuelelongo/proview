// Package cloud coordinates signature synchronization independently of the UI.
package cloud

import (
	"context"
	"emanuelelongo/proview/pkg/signatures"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Provider owns authentication and remote folder layout. Listing is complete
// (including pagination); uploads must be idempotent by operation ID.
type Provider interface {
	Destination() string
	Prepare(context.Context) error
	Download(context.Context) ([]signatures.Operation, error)
	Upload(context.Context, signatures.Operation) error
}

// CurrentStateProvider exchanges compact state rather than an operation history.
type CurrentStateProvider interface {
	SyncCurrent(context.Context, *signatures.Store) error
}

type Status struct {
	Connected bool
	Busy      bool
	Pending   int
	LastSync  time.Time
	Err       error
}

type Coordinator struct {
	Store        *signatures.Store
	Now          func() time.Time
	mu           sync.Mutex
	run          sync.Mutex
	provider     Provider
	lastAttempt  time.Time
	status       Status
	generation   uint64
	cancel       context.CancelFunc
	listeners    map[uint64]func(Status)
	nextListener uint64
	jobs         int
}

func (c *Coordinator) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}
func (c *Coordinator) Report(err error) { c.mu.Lock(); c.status.Err = err; c.mu.Unlock(); c.publish() }

func (c *Coordinator) Status() Status { c.mu.Lock(); defer c.mu.Unlock(); return c.status }

// Subscribe delivers status outside locks; callbacks must dispatch UI work to
// its main thread. Removing a listener never cancels a shared sync operation.
func (c *Coordinator) Subscribe(fn func(Status)) func() {
	c.mu.Lock()
	if c.listeners == nil {
		c.listeners = map[uint64]func(Status){}
	}
	c.nextListener++
	id := c.nextListener
	c.listeners[id] = fn
	c.mu.Unlock()
	return func() { c.mu.Lock(); delete(c.listeners, id); c.mu.Unlock() }
}
func (c *Coordinator) publish() {
	c.mu.Lock()
	status := c.status
	callbacks := make([]func(Status), 0, len(c.listeners))
	for _, fn := range c.listeners {
		callbacks = append(callbacks, fn)
	}
	c.mu.Unlock()
	for _, fn := range callbacks {
		fn(status)
	}
}

func (c *Coordinator) Connect(p Provider) {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.provider = p
	c.generation++
	c.lastAttempt = time.Time{}
	c.status = Status{Connected: true}
	c.jobs = 0
	c.mu.Unlock()
	c.publish()
}
func (c *Coordinator) Disconnect() {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
	}
	c.provider = nil
	c.generation++
	c.status = Status{}
	c.jobs = 0
	c.mu.Unlock()
	c.publish()
}

// Open is the only normal trigger for downloads. Attempts, even failed ones,
// are throttled across windows. Connect explicitly resets the throttle.
func (c *Coordinator) Open(ctx context.Context) bool {
	c.mu.Lock()
	if c.provider == nil || c.status.Busy || (!c.lastAttempt.IsZero() && c.now().Sub(c.lastAttempt) < 30*time.Second) {
		c.mu.Unlock()
		return false
	}
	c.lastAttempt = c.now()
	c.status.Busy = true
	c.jobs++
	gen := c.generation
	p := c.provider
	work, cancel := context.WithCancel(ctx)
	c.cancel = cancel
	c.mu.Unlock()
	c.publish()
	go c.execute(work, cancel, p, gen, true)
	return true
}

// Push bypasses the full-sync throttle and waits behind an active sync.
// Current-state providers fetch changed remote state before merging an upload.
func (c *Coordinator) Push(ctx context.Context) {
	c.mu.Lock()
	p := c.provider
	gen := c.generation
	if p != nil {
		c.jobs++
		c.status.Busy = true
	}
	c.mu.Unlock()
	if p == nil {
		return
	}
	c.publish()
	go func() {
		c.run.Lock()
		defer c.run.Unlock()
		c.mu.Lock()
		if gen != c.generation {
			c.mu.Unlock()
			return
		}
		c.status.Busy = true
		work, cancel := context.WithCancel(ctx)
		c.cancel = cancel
		c.mu.Unlock()
		c.publish()
		defer cancel()
		err := c.Store.EnableSync()
		if err == nil {
			err = p.Prepare(work)
		}
		if err == nil {
			if current, ok := p.(CurrentStateProvider); ok {
				err = current.SyncCurrent(work, c.Store)
			} else {
				err = c.flush(work, p)
			}
		}
		c.finish(p, gen, false, err)
	}()
}

func (c *Coordinator) execute(ctx context.Context, cancel context.CancelFunc, p Provider, gen uint64, full bool) {
	c.run.Lock()
	defer c.run.Unlock()
	defer cancel()
	c.mu.Lock()
	if gen != c.generation {
		c.mu.Unlock()
		return
	}
	c.cancel = cancel
	c.mu.Unlock()
	var failures []error
	if err := c.Store.EnableSync(); err != nil {
		c.finish(p, gen, false, err)
		return
	}
	if err := p.Prepare(ctx); err != nil {
		c.finish(p, gen, false, err)
		return
	}
	if current, ok := p.(CurrentStateProvider); ok {
		err := current.SyncCurrent(ctx, c.Store)
		c.finish(p, gen, full, err)
		return
	}
	if full {
		ops, err := p.Download(ctx)
		failures = append(failures, err)
		if ctx.Err() == nil {
			mergeErr := c.Store.Merge(ops)
			failures = append(failures, mergeErr)
			// Already present operations need not be uploaded again.
			for _, op := range ops {
				if mergeErr == nil && op.Validate() == nil {
					failures = append(failures, c.Store.Acknowledge(p.Destination(), op.ID))
				}
			}
		}
	}
	if ctx.Err() == nil {
		failures = append(failures, c.flush(ctx, p))
	} else {
		failures = append(failures, ctx.Err())
	}
	c.finish(p, gen, full, errors.Join(failures...))
}
func (c *Coordinator) flush(ctx context.Context, p Provider) error {
	pending, err := c.Store.Pending(p.Destination())
	if err != nil {
		return err
	}
	for _, op := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := p.Upload(ctx, op); err != nil {
			return fmt.Errorf("upload signature: %w", err)
		}
		if err := c.Store.Acknowledge(p.Destination(), op.ID); err != nil {
			return err
		}
	}
	return nil
}
func (c *Coordinator) finish(p Provider, gen uint64, full bool, err error) {
	pending, pendingErr := c.Store.Pending(p.Destination())
	c.mu.Lock()
	if gen != c.generation {
		c.mu.Unlock()
		return
	}
	c.jobs--
	c.status.Busy = c.jobs > 0
	c.status.Pending = len(pending)
	c.status.Err = errors.Join(err, pendingErr)
	if full && err == nil {
		c.status.LastSync = c.now()
	}
	c.cancel = nil
	c.mu.Unlock()
	c.publish()
}
