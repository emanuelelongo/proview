package cloud

import (
	"context"
	"emanuelelongo/proview/pkg/signatures"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeProvider struct {
	mu         sync.Mutex
	name       string
	operations map[string]signatures.Operation
	downloads  int
	uploads    int
	fail       bool
	entered    chan struct{}
	release    chan struct{}
}

func (p *fakeProvider) Destination() string           { return p.name }
func (p *fakeProvider) Prepare(context.Context) error { return nil }
func (p *fakeProvider) Download(ctx context.Context) ([]signatures.Operation, error) {
	if p.entered != nil {
		select {
		case p.entered <- struct{}{}:
		default:
		}
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.downloads++
	if p.fail {
		return nil, errors.New("offline")
	}
	var result []signatures.Operation
	for _, op := range p.operations {
		result = append(result, op)
	}
	return result, nil
}
func (p *fakeProvider) Upload(_ context.Context, op signatures.Operation) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.fail {
		return errors.New("offline")
	}
	if p.operations == nil {
		p.operations = map[string]signatures.Operation{}
	}
	if _, ok := p.operations[op.ID]; !ok {
		p.operations[op.ID] = op
		p.uploads++
	}
	return nil
}
func asset(t *testing.T) signatures.Asset {
	t.Helper()
	a, err := signatures.Import("s.svg", []byte(`<svg viewBox="0 0 10 10"><path d="M1 1L9 9" stroke="black"/></svg>`))
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func save(t *testing.T, store *signatures.Store, name string) signatures.Entry {
	t.Helper()
	entry, err := store.Save(signatures.Entry{Asset: asset(t)})
	if err != nil {
		t.Fatal(err)
	}
	return entry
}
func waitIdle(t *testing.T, c *Coordinator) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !c.Status().Busy {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("sync never completed")
}
func TestThrottleFailedAttemptsImmediateUploadAndRestart(t *testing.T) {
	store := &signatures.Store{Root: t.TempDir()}
	save(t, store, "Original")
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Writes to the fake clock occur only after all jobs have completed.
	var clockMu sync.Mutex
	c := &Coordinator{Store: store, Now: func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }}
	advance := func(d time.Duration) { clockMu.Lock(); now = now.Add(d); clockMu.Unlock() }
	p := &fakeProvider{name: "drive-a", fail: true}
	c.Connect(p)
	if !c.Open(context.Background()) {
		t.Fatal("initial sync not started")
	}
	waitIdle(t, c)
	if c.Status().Err == nil || c.Status().Pending != 1 {
		t.Fatal("offline operation not queued")
	}
	if c.Open(context.Background()) {
		t.Fatal("failed attempt was not throttled")
	}
	advance(29 * time.Second)
	if c.Open(context.Background()) {
		t.Fatal("sync started before 30 seconds")
	}
	p.mu.Lock()
	p.fail = false
	p.mu.Unlock()
	entry, err := store.Save(signatures.Entry{Asset: asset(t)})
	if err != nil {
		t.Fatal(err)
	}
	c.Push(context.Background())
	waitIdle(t, c)
	p.mu.Lock()
	downloads, uploads := p.downloads, p.uploads
	p.mu.Unlock()
	if downloads != 1 || uploads != 2 || c.Status().Pending != 0 {
		t.Fatalf("push downloaded or queue did not flush: %d %d", downloads, uploads)
	}
	entry.Asset = entry.Asset.Clone()
	entry.Asset.Data = []byte(`<svg viewBox="0 0 20 10"><path d="M1 1L19 9" fill="none" stroke="black"/></svg>`)
	entry.Asset.Kind = signatures.SVG
	if _, err = store.Save(entry); err != nil {
		t.Fatal(err)
	}
	// Restart coordinator and store before uploading the edit.
	c.Disconnect()
	store = &signatures.Store{Root: store.Root}
	next := &Coordinator{Store: store}
	next.Connect(p)
	next.Open(context.Background())
	waitIdle(t, next)
	if next.Status().Err != nil || next.Status().Pending != 0 {
		t.Fatalf("retry failed: %+v", next.Status())
	}
	entries, err := store.List()
	if err != nil || len(entries) != 1 || signatures.Fingerprint(entries[0].Asset) != signatures.Fingerprint(entry.Asset) {
		t.Fatalf("restart lost edit: %+v %v", entries, err)
	}
	advance(time.Second)
	c.Connect(p)
	if !c.Open(context.Background()) {
		t.Fatal("activation did not reset throttle")
	}
	waitIdle(t, c)
}
func TestSharedCoordinatorConcurrentOpenAndQueuedPush(t *testing.T) {
	store := &signatures.Store{Root: t.TempDir()}
	save(t, store, "First")
	p := &fakeProvider{name: "drive", entered: make(chan struct{}, 1), release: make(chan struct{})}
	c := &Coordinator{Store: store}
	c.Connect(p)
	var wg sync.WaitGroup
	var mu sync.Mutex
	started := 0
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if c.Open(context.Background()) {
				mu.Lock()
				started++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if started != 1 {
		t.Fatalf("started %d downloads", started)
	}
	select {
	case <-p.entered:
	case <-time.After(time.Second):
		t.Fatal("download not entered")
	}
	entry := save(t, store, "New name")
	c.Push(context.Background())
	close(p.release)
	waitIdle(t, c)
	if c.Status().Err != nil {
		t.Fatal(c.Status().Err)
	}
	p.mu.Lock()
	downloads := p.downloads
	remoteCount := len(p.operations)
	p.mu.Unlock()
	if downloads != 1 || remoteCount != 2 {
		t.Fatalf("lost queued save: downloads=%d operations=%d", downloads, remoteCount)
	}
	if entries, _ := store.List(); len(entries) != 1 || !entries[0].Updated.Equal(entry.Updated) {
		t.Fatal("merge overwrote local change")
	}
}
func TestDisconnectCancelsDownloadAndKeepsLocalQueue(t *testing.T) {
	store := &signatures.Store{Root: t.TempDir()}
	save(t, store, "Offline")
	p := &fakeProvider{name: "drive", entered: make(chan struct{}, 1), release: make(chan struct{})}
	c := &Coordinator{Store: store}
	c.Connect(p)
	c.Open(context.Background())
	select {
	case <-p.entered:
	case <-time.After(time.Second):
		t.Fatal("download not started")
	}
	c.Disconnect()
	c.run.Lock()
	c.run.Unlock()
	if status := c.Status(); status.Connected || status.Busy || status.Err != nil {
		t.Fatalf("stale job changed disconnected status: %+v", status)
	}
	pending, err := store.Pending(p.name)
	if err != nil || len(pending) != 1 {
		t.Fatal("disconnect lost pending signature")
	}
	if c.Open(context.Background()) {
		t.Fatal("sync started while disconnected")
	}
}
