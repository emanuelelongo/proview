package signatures

import (
	"testing"
	"time"
)

func TestCurrentSyncStateKeepsLatestDeletionAndAliases(t *testing.T) {
	store := syncStore(t)
	first := syncSave(t, store, Entry{Asset: syncAsset(t, 4)})
	if _, err := store.Save(Entry{Asset: syncAsset(t, 6)}); err != nil {
		t.Fatal(err)
	}
	ops, _ := store.Operations()
	// Simulate a second installation independently importing the same asset.
	duplicate := ops[0]
	for _, op := range ops {
		if op.Entry != nil && op.Entry.ID == first.ID {
			duplicate = op
		}
	}
	duplicate.ID, _ = newID()
	duplicate.Identity = Fingerprint(syncAsset(t, 8))
	duplicate.Entry = ptrEntry(*duplicate.Entry)
	duplicate.Entry.ID, _ = newID()
	duplicate.Updated = duplicate.Updated.Add(-time.Second)
	duplicate.Entry.Updated = duplicate.Updated
	if err := store.Merge([]Operation{duplicate}); err != nil {
		t.Fatal(err)
	}
	state, err := store.CurrentSyncState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Operations) != 2 || len(state.Aliases) != 1 {
		t.Fatalf("state: %d operations, %d aliases", len(state.Operations), len(state.Aliases))
	}
	// Aliasing must survive after the winning asset itself changes.
	entries, _ := store.List()
	var edited Entry
	for _, entry := range entries {
		if Fingerprint(entry.Asset) == duplicate.Content {
			edited = entry
		}
	}
	edited.Asset = syncAsset(t, 9)
	if _, err = store.Save(edited); err != nil {
		t.Fatal(err)
	}
	state, err = store.CurrentSyncState()
	if err != nil {
		t.Fatal(err)
	}
	fresh := syncStore(t)
	if err = fresh.MergeSyncState(state); err != nil {
		t.Fatal(err)
	}
	if err = fresh.MergeSyncState(SyncState{Version: 1, Operations: []Operation{duplicate}}); err != nil {
		t.Fatal(err)
	}
	got, _ := fresh.List()
	if len(got) != 2 {
		t.Fatal("historical alias split after compact-state download")
	}
	reopened := &Store{Root: fresh.Root}
	got, _ = reopened.List()
	if len(got) != 2 {
		t.Fatal("alias persistence failed on restart")
	}
	if err = store.Delete(edited.ID); err != nil {
		t.Fatal(err)
	}
	state, _ = store.CurrentSyncState()
	if err = fresh.MergeSyncState(state); err != nil {
		t.Fatal(err)
	}
	if err = fresh.MergeSyncState(SyncState{Version: 1, Operations: []Operation{duplicate}}); err != nil {
		t.Fatal(err)
	}
	got, _ = fresh.List()
	if len(got) != 1 {
		t.Fatal("offline alias restored a deleted signature")
	}
}
func ptrEntry(e Entry) *Entry { return &e }
