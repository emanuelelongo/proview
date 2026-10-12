package signatures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func syncAsset(t *testing.T, width float64) Asset {
	t.Helper()
	drawing := exampleDrawing()
	drawing.Width = width
	asset, err := FromDrawing(drawing)
	if err != nil {
		t.Fatal(err)
	}
	return asset
}
func syncStore(t *testing.T) *Store {
	t.Helper()
	store := &Store{Root: t.TempDir()}
	if err := store.EnableSync(); err != nil {
		t.Fatal(err)
	}
	return store
}
func syncSave(t *testing.T, store *Store, entry Entry) Entry {
	t.Helper()
	saved, err := store.Save(entry)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}
func exchange(t *testing.T, a, b *Store) {
	t.Helper()
	left, err := a.Operations()
	if err != nil {
		t.Fatal(err)
	}
	right, err := b.Operations()
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Merge(right); err != nil {
		t.Fatal(err)
	}
	if err = b.Merge(left); err != nil {
		t.Fatal(err)
	}
}
func single(t *testing.T, store *Store) Entry {
	t.Helper()
	entries, err := store.List()
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%d err=%v", len(entries), err)
	}
	return entries[0]
}
func TestSyncBootstrapDeduplicatesAndKeepsDrawing(t *testing.T) {
	asset := syncAsset(t, 4)
	a, b := &Store{Root: t.TempDir()}, &Store{Root: t.TempDir()}
	before := syncSave(t, a, Entry{Asset: asset})
	syncSave(t, b, Entry{Asset: asset})
	for _, store := range []*Store{a, b} {
		if err := store.EnableSync(); err != nil {
			t.Fatal(err)
		}
	}
	if !single(t, a).Updated.Equal(before.Updated) {
		t.Fatal("bootstrap changed original timestamp")
	}
	exchange(t, a, b)
	left, right := single(t, a), single(t, b)
	if left.ID != right.ID || !left.Updated.Equal(right.Updated) || left.Asset.Drawing == nil {
		t.Fatal("duplicates did not converge with editable drawing")
	}
	count, _ := a.Operations()
	if err := a.EnableSync(); err != nil {
		t.Fatal(err)
	}
	again, _ := a.Operations()
	if len(count) != len(again) {
		t.Fatal("bootstrap repeated")
	}
}
func TestSyncLatestEditDeletionAndOfflineRecovery(t *testing.T) {
	a, b := syncStore(t), syncStore(t)
	entry := syncSave(t, a, Entry{Asset: syncAsset(t, 4)})
	exchange(t, a, b)
	entry.Asset = syncAsset(t, 4.5)
	syncSave(t, a, entry)
	other := single(t, b)
	other.Asset = syncAsset(t, 5)
	syncSave(t, b, other)
	exchange(t, a, b)
	if Fingerprint(single(t, a).Asset) != Fingerprint(other.Asset) {
		t.Fatal("latest edit did not win")
	}
	if err := a.Delete(single(t, a).ID); err != nil {
		t.Fatal(err)
	}
	// Reopen the app while offline: deletion and pending uploads survive.
	a = &Store{Root: a.Root}
	pending, err := a.Pending("account-a")
	if err != nil || len(pending) == 0 {
		t.Fatalf("missing durable queue: %v", err)
	}
	entries, err := a.List()
	if err != nil || len(entries) != 0 {
		t.Fatalf("deleted signature reappeared: %v", err)
	}
	exchange(t, a, b)
	exchange(t, a, b)
	entries, err = b.List()
	if err != nil || len(entries) != 0 {
		t.Fatal("remote signature resurrected")
	}
}
func TestSyncContentAliasesSurviveEditAndDeletion(t *testing.T) {
	a, b := syncStore(t), syncStore(t)
	syncSave(t, a, Entry{Asset: syncAsset(t, 4)})
	syncSave(t, b, Entry{Asset: syncAsset(t, 5)})
	exchange(t, a, b)
	entries, _ := b.List()
	var edit Entry
	for _, entry := range entries {
		if entry.Asset.Drawing.Width == 5 {
			edit = entry
		}
	}
	edit.Asset = syncAsset(t, 4)
	syncSave(t, b, edit)
	exchange(t, a, b)
	winner := single(t, a)
	winner.Asset = syncAsset(t, 6)
	syncSave(t, a, winner)
	exchange(t, a, b)
	if Fingerprint(single(t, b).Asset) != Fingerprint(winner.Asset) {
		t.Fatal("alias disappeared after content edit")
	}
	if err := b.Delete(single(t, b).ID); err != nil {
		t.Fatal(err)
	}
	exchange(t, a, b)
	entries, _ = a.List()
	if len(entries) != 0 {
		t.Fatal("aliased signature resurrected")
	}
}
func TestSyncTieBreakAndInvalidRecordIsolation(t *testing.T) {
	store := syncStore(t)
	asset := syncAsset(t, 4)
	stamp := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := Entry{Version: 1, ID: "11111111111111111111111111111111", Created: stamp, Updated: stamp, Asset: asset}
	second := first
	second.ID = "22222222222222222222222222222222"
	second.Created = stamp.Add(-time.Second)
	a := Operation{Version: 1, ID: first.ID, Identity: Fingerprint(asset), Content: Fingerprint(asset), Updated: stamp, Entry: &first}
	b := a
	b.ID = second.ID
	b.Entry = &second
	invalid := a
	invalid.ID = "33333333333333333333333333333333"
	invalid.Content = "invalid"
	if err := store.Merge([]Operation{b, invalid, a}); err == nil {
		t.Fatal("corrupt record not reported")
	}
	if single(t, store).Created != second.Created {
		t.Fatal("tie break did not use operation ID")
	}
	if err := store.Merge([]Operation{a, b}); err != nil {
		t.Fatal(err)
	}
	if single(t, store).Created != second.Created {
		t.Fatal("merge order changed winner")
	}
	tampered := second
	tampered.Created = tampered.Created.Add(time.Second)
	b.Entry = &tampered
	if err := store.Merge([]Operation{b}); err == nil {
		t.Fatal("accepted operation ID collision")
	}
	if single(t, store).Created != second.Created {
		t.Fatal("collision overwrote valid signature")
	}
}
func TestSyncReplaysInterruptedMaterializationAndScopesAcknowledgements(t *testing.T) {
	store := syncStore(t)
	entry := syncSave(t, store, Entry{Asset: syncAsset(t, 4)})
	ops, err := store.Operations()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a committed operation with an interrupted archive update.
	entry.Asset = syncAsset(t, 7)
	entry.Updated = entry.Updated.Add(time.Second)
	op, err := saveOperation(entry, ops[0].Identity)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.writeOperation(op); err != nil {
		t.Fatal(err)
	}
	reopened := &Store{Root: store.Root}
	if single(t, reopened).Asset.Drawing.Width != 7 {
		t.Fatal("journal was not replayed")
	}
	if err = reopened.Acknowledge("folder-a", op.ID); err != nil {
		t.Fatal(err)
	}
	pendingA, _ := reopened.Pending("folder-a")
	pendingB, _ := reopened.Pending("folder-b")
	if len(pendingA) != 1 || len(pendingB) != 2 {
		t.Fatal("destination upload acknowledgements leaked")
	}
	// Repair a damaged current.json from the complete journal.
	if err = os.WriteFile(filepath.Join(store.Root, entry.ID, "current.json"), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if single(t, &Store{Root: store.Root}).Asset.Drawing.Width != 7 {
		t.Fatal("materialization was not repaired")
	}
}
func TestSyncHashIgnoresEntryMetadataButPreservesOriginalStrokes(t *testing.T) {
	a := syncAsset(t, 4)
	b := a.Clone()
	b.Drawing.Strokes[0][0].X += 0.00000001
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatal("fingerprint lost original stroke precision")
	}
	data, _ := json.Marshal(Operation{Version: 2})
	if _, err := DecodeOperation(data); err == nil {
		t.Fatal("accepted future protocol version")
	}
	if _, err := DecodeOperation(make([]byte, MaxOperationSize+1)); err == nil {
		t.Fatal("accepted oversized operation")
	}
}
