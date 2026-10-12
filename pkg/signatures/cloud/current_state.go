package cloud

import (
	"context"
	"crypto/sha256"
	"emanuelelongo/proview/pkg/signatures"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
)

const maxCurrentIndexSize = 4 << 20

type currentStateRecord struct {
	Operation signatures.Operation `json:"operation"` // Header only: Entry is stored separately.
	FileID    string               `json:"file,omitempty"`
	Hash      string               `json:"sha256,omitempty"`
}
type currentStateIndex struct {
	Version int                  `json:"version"`
	Aliases map[string]string    `json:"aliases,omitempty"`
	Retired []string             `json:"retired,omitempty"`
	Records []currentStateRecord `json:"records"`
}

func stateIdentity(id string, aliases map[string]string) string {
	for n := 0; n <= len(aliases); n++ {
		next, ok := aliases[id]
		if !ok || next == id {
			return id
		}
		id = next
	}
	return id
}
func mergeStateAliases(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for x, y := range a {
		out[x] = y
	}
	for x, y := range b {
		if previous, ok := out[x]; ok && previous != y {
			out[previous] = y
		} else {
			out[x] = y
		}
	}
	return out
}
func operationHash(op signatures.Operation) string {
	hash := sha256.Sum256(mustJSON(op))
	return hex.EncodeToString(hash[:])
}
func parallelTransfers(count int, fn func(int) error) error {
	var wg sync.WaitGroup
	failures := make([]error, count)
	jobs := make(chan int)
	for n := 0; n < min(4, count); n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				failures[i] = fn(i)
			}
		}()
	}
	for i := 0; i < count; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return errors.Join(failures...)
}
func validStateHex(value string, size int) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == size && strings.ToLower(value) == value
}
func validateCurrentIndex(index currentStateIndex) error {
	if index.Version != 2 || len(index.Records) > 10000 {
		return errors.New("unsupported signature index")
	}
	if _, err := signatures.CompactSyncState(nil, index.Aliases); err != nil {
		return err
	}
	seen := map[string]bool{}
	identities := map[string]bool{}
	for _, record := range index.Records {
		op := record.Operation
		identity := stateIdentity(op.Identity, index.Aliases)
		if op.Version != 1 || !validStateHex(op.ID, 16) || !validStateHex(op.Identity, 32) || op.Updated.IsZero() || identities[identity] {
			return errors.New("invalid signature index record")
		}
		identities[identity] = true
		if op.Entry != nil || seen[op.ID] {
			return errors.New("invalid signature index record")
		}
		seen[op.ID] = true
		if op.Deleted {
			if err := op.Validate(); err != nil {
				return err
			}
			if record.FileID != "" || record.Hash != "" {
				return errors.New("invalid signature tombstone")
			}
		} else if record.FileID == "" || !validStateHex(record.Hash, 32) || !validStateHex(op.Content, 32) {
			return errors.New("invalid signature payload reference")
		}
	}
	return nil
}

// Acknowledge only operations superseded by the state actually published.
func acknowledgeCurrent(ctx context.Context, store *signatures.Store, destination string, state signatures.SyncState) error {
	pending, err := store.Pending(destination)
	if err != nil {
		return err
	}
	winners := map[string]signatures.Operation{}
	for _, op := range state.Operations {
		winners[stateIdentity(op.Identity, state.Aliases)] = op
	}
	for _, op := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}
		winner, ok := winners[stateIdentity(op.Identity, state.Aliases)]
		if ok && (op.Updated.Before(winner.Updated) || op.Updated.Equal(winner.Updated) && op.ID <= winner.ID) {
			if err := store.Acknowledge(destination, op.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
