package signatures

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

// SyncState retains one winning snapshot or tombstone per logical signature.
// Aliases preserve past exact-content deduplication without retaining old assets.
type SyncState struct {
	Version    int               `json:"version"`
	Operations []Operation       `json:"operations"`
	Aliases    map[string]string `json:"aliases,omitempty"`
}

func CompactSyncState(ops []Operation, aliases map[string]string) (SyncState, error) {
	for _, op := range ops {
		if err := op.Validate(); err != nil {
			return SyncState{}, err
		}
	}
	for a, b := range aliases {
		if !validHash(a) || !validHash(b) {
			return SyncState{}, errors.New("invalid signature aliases")
		}
	}
	winners, resolved, _ := resolveWithAliases(ops, aliases)
	state := SyncState{Version: 1, Aliases: map[string]string{}}
	for a, b := range resolved {
		if a != b {
			state.Aliases[a] = b
		}
	}
	for _, op := range winners {
		state.Operations = append(state.Operations, op)
	}
	sort.Slice(state.Operations, func(i, j int) bool { return state.Operations[i].Identity < state.Operations[j].Identity })
	return state, nil
}
func (s *Store) CurrentSyncState() (SyncState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ops, err := s.operations()
	if err != nil {
		return SyncState{}, err
	}
	return CompactSyncState(ops, s.syncAliases)
}
func (s *Store) MergeSyncState(state SyncState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.syncReady || state.Version != 1 {
		return errors.New("invalid signature sync state")
	}
	if _, err := CompactSyncState(state.Operations, state.Aliases); err != nil {
		return err
	}
	ops, err := s.operations()
	if err != nil {
		return err
	}
	aliases := map[string]string{}
	for a, b := range s.syncAliases {
		aliases[a] = b
	}
	// Resolve both sets as edges rather than replacing conflicting alias targets.
	_, aliases, _ = resolveWithAliases(append(ops, state.Operations...), mergeAliasEdges(aliases, state.Aliases))
	for _, op := range state.Operations {
		if err := s.writeOperation(op); err != nil {
			return err
		}
	}
	data, err := json.Marshal(aliases)
	if err != nil {
		return err
	}
	if err = writeAtomic(filepath.Join(s.syncDir(), "aliases.json"), data); err != nil {
		return err
	}
	s.syncAliases = aliases
	return s.materialize()
}
func mergeAliasEdges(a, b map[string]string) map[string]string {
	// Existing aliases are canonical; chaining an overlapping edge retains both.
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
func (s *Store) loadSyncAliases() error {
	data, err := os.ReadFile(filepath.Join(s.syncDir(), "aliases.json"))
	if errors.Is(err, os.ErrNotExist) {
		s.syncAliases = nil
		return nil
	}
	if err != nil {
		return err
	}
	var aliases map[string]string
	if err = json.Unmarshal(data, &aliases); err != nil {
		return err
	}
	for a, b := range aliases {
		if !validHash(a) || !validHash(b) {
			return errors.New("invalid signature aliases")
		}
	}
	s.syncAliases = aliases
	return nil
}
