package signatures

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestLegacyEntryRoundTripAndNamelessSave(t *testing.T) {
	store := syncStore(t)
	entry := syncSave(t, store, Entry{Asset: syncAsset(t, 4)})
	legacy := entryWire{entry.Version, entry.ID, "Old signature", entry.Created, entry.Updated, entry.Asset}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Entry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	roundTrip, _ := json.Marshal(decoded)
	if !bytes.Equal(roundTrip, data) {
		t.Fatal("legacy payload bytes changed, invalidating cloud hashes")
	}
	op, err := saveOperation(decoded, Fingerprint(decoded.Asset))
	if err != nil || op.Validate() != nil {
		t.Fatal("legacy cloud operation was rejected")
	}
	if err := store.Merge([]Operation{op}); err != nil {
		t.Fatal(err)
	}
	saved, err := store.Save(decoded)
	if err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(saved)
	if bytes.Contains(plain, []byte(`"name"`)) {
		t.Fatal("new saves still contain a signature name")
	}
	entries, err := (&Store{Root: store.Root}).List()
	if err != nil || len(entries) != 1 {
		t.Fatal("nameless entry could not be reopened")
	}
}
