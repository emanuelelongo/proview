package signatures

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Operation is an immutable snapshot or tombstone. Identity survives edits;
// Content identifies exact assets independently of their local ID and name.
type Operation struct {
	Version  int       `json:"version"`
	ID       string    `json:"id"`
	Identity string    `json:"identity"`
	Content  string    `json:"content,omitempty"`
	Updated  time.Time `json:"updated"`
	Deleted  bool      `json:"deleted,omitempty"`
	Entry    *Entry    `json:"entry,omitempty"`
}

const MaxOperationSize = MaxFileSize * 2

func Fingerprint(asset Asset) string {
	// Struct field order and byte encoding are stable; no map or incidental metadata.
	data, _ := json.Marshal(struct {
		Version int   `json:"version"`
		Asset   Asset `json:"asset"`
	}{1, asset})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func validHash(value string) bool {
	data, err := hex.DecodeString(value)
	return err == nil && len(data) == 32 && value == strings.ToLower(value)
}

func (op Operation) Validate() error {
	if op.Version != 1 || !validID(op.ID) || !validHash(op.Identity) || op.Updated.IsZero() {
		return errors.New("invalid sync operation")
	}
	if op.Deleted {
		if op.Entry != nil || op.Content != "" {
			return errors.New("invalid sync tombstone")
		}
		return nil
	}
	if op.Entry == nil || op.Entry.Version != 1 || !validID(op.Entry.ID) || op.Entry.Created.IsZero() || !op.Entry.Updated.Equal(op.Updated) {
		return errors.New("invalid sync signature")
	}
	if err := op.Entry.Asset.Validate(); err != nil {
		return err
	}
	if !validHash(op.Content) || op.Content != Fingerprint(op.Entry.Asset) {
		return errors.New("signature hash mismatch")
	}
	return nil
}

func DecodeOperation(data []byte) (Operation, error) {
	var op Operation
	if len(data) > MaxOperationSize {
		return op, errors.New("sync operation is too large")
	}
	if err := json.Unmarshal(data, &op); err != nil {
		return op, err
	}
	return op, op.Validate()
}

func (s *Store) syncDir() string { return filepath.Join(s.Root, ".sync") }

// EnableSync bootstraps existing entries once. The journal subsequently acts as
// a write-ahead log: after a crash the materialized archive is reconstructed.
func (s *Store) EnableSync() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverSync(); err != nil {
		return err
	}
	if s.syncReady {
		return nil
	}
	entries, err := s.list()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Join(s.syncDir(), "operations"), 0700); err != nil {
		return err
	}
	for _, entry := range entries {
		op, err := saveOperation(entry, Fingerprint(entry.Asset))
		if err != nil {
			return err
		}
		if err = s.writeOperation(op); err != nil {
			return err
		}
	}
	if err = writeAtomic(filepath.Join(s.syncDir(), "enabled"), []byte("1")); err != nil {
		return err
	}
	s.syncReady = true
	return s.materialize()
}

func saveOperation(entry Entry, identity string) (Operation, error) {
	id, err := newID()
	return Operation{Version: 1, ID: id, Identity: identity, Content: Fingerprint(entry.Asset), Updated: entry.Updated, Entry: &entry}, err
}

func (s *Store) writeOperation(op Operation) error {
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	if len(data) > MaxOperationSize {
		return errors.New("sync operation is too large")
	}
	path := filepath.Join(s.syncDir(), "operations", op.ID+".json")
	if previous, err := os.ReadFile(path); err == nil {
		if string(previous) != string(data) {
			return errors.New("conflicting sync operation ID")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeAtomic(path, data)
}

func (s *Store) operations() ([]Operation, error) {
	if err := s.loadSyncAliases(); err != nil {
		return nil, err
	}
	dirs, err := os.ReadDir(filepath.Join(s.syncDir(), "operations"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []Operation
	for _, file := range dirs {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		info, err := file.Info()
		if err != nil {
			return nil, err
		}
		if info.Size() > MaxOperationSize {
			return nil, errors.New("local sync operation is too large")
		}
		data, err := os.ReadFile(filepath.Join(s.syncDir(), "operations", file.Name()))
		if err != nil {
			return nil, err
		}
		op, err := DecodeOperation(data)
		if err != nil {
			return nil, fmt.Errorf("local sync journal: %w", err)
		}
		if file.Name() != op.ID+".json" {
			return nil, errors.New("local sync operation filename mismatch")
		}
		result = append(result, op)
	}
	return result, nil
}

func (s *Store) Operations() ([]Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.operations()
}

// Group identities whenever any valid snapshot shares exact content. Retaining
// the immutable snapshots makes these aliases persistent on every device.
func resolve(ops []Operation) (map[string]Operation, map[string]string, map[string]string) {
	return resolveWithAliases(ops, nil)
}
func resolveWithAliases(ops []Operation, aliases map[string]string) (map[string]Operation, map[string]string, map[string]string) {
	parent := map[string]string{}
	var root func(string) string
	root = func(id string) string {
		p, ok := parent[id]
		if !ok {
			parent[id] = id
			return id
		}
		if p != id {
			parent[id] = root(p)
		}
		return parent[id]
	}
	for a, b := range aliases {
		x, y := root(a), root(b)
		if x > y {
			x, y = y, x
		}
		parent[y] = x
	}
	hashes := map[string]string{}
	for _, op := range ops {
		root(op.Identity)
		if op.Deleted {
			continue
		}
		if previous, ok := hashes[op.Content]; ok {
			a, b := root(op.Identity), root(previous)
			if a > b {
				a, b = b, a
			}
			parent[b] = a
		} else {
			hashes[op.Content] = op.Identity
		}
	}
	winners := map[string]Operation{}
	ids := map[string]string{}
	for _, op := range ops {
		key := root(op.Identity)
		previous, ok := winners[key]
		if !ok || op.Updated.After(previous.Updated) || (op.Updated.Equal(previous.Updated) && op.ID > previous.ID) {
			winners[key] = op
		}
		if op.Entry != nil && (ids[key] == "" || op.Entry.ID < ids[key]) {
			ids[key] = op.Entry.ID
		}
	}
	aliases = map[string]string{}
	for id := range parent {
		aliases[id] = root(id)
	}
	return winners, aliases, ids
}

func (s *Store) identityFor(id string, ops []Operation) string {
	_, aliases, ids := resolveWithAliases(ops, s.syncAliases)
	for _, op := range ops {
		if op.Entry != nil && (op.Entry.ID == id || ids[aliases[op.Identity]] == id) {
			return aliases[op.Identity]
		}
	}
	return ""
}

func (s *Store) recordSave(entry Entry) error {
	ops, err := s.operations()
	if err != nil {
		return err
	}
	identity := s.identityFor(entry.ID, ops)
	if identity == "" {
		identity = Fingerprint(entry.Asset)
	}
	op, err := saveOperation(entry, identity)
	if err != nil {
		return err
	}
	return s.writeOperation(op)
}

func (s *Store) recordDelete(id string) error {
	if _, err := s.read(id); err != nil {
		return err
	}
	ops, err := s.operations()
	if err != nil {
		return err
	}
	identity := s.identityFor(id, ops)
	if identity == "" {
		return errors.New("signature has no sync identity")
	}
	opID, err := newID()
	if err != nil {
		return err
	}
	return s.writeOperation(Operation{Version: 1, ID: opID, Identity: identity, Updated: time.Now().UTC(), Deleted: true})
}

func (s *Store) recoverSync() error {
	if !s.syncReady {
		_, err := os.Stat(filepath.Join(s.syncDir(), "enabled"))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		s.syncReady = true
	}
	return s.materialize()
}

func (s *Store) materialize() error {
	ops, err := s.operations()
	if err != nil {
		return err
	}
	winners, _, ids := resolveWithAliases(ops, s.syncAliases)
	desired := map[string]Entry{}
	for identity, op := range winners {
		if op.Deleted {
			continue
		}
		entry := *op.Entry
		entry.ID = ids[identity]
		desired[entry.ID] = entry
	}
	// Read directories directly: a damaged materialization can be repaired from
	// the journal, without preventing access to the other signatures.
	dirs, err := os.ReadDir(s.Root)
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		if dir.IsDir() && validID(dir.Name()) {
			if _, ok := desired[dir.Name()]; !ok {
				if err = s.delete(dir.Name()); err != nil {
					return err
				}
			}
		}
	}
	for id, entry := range desired {
		previous, err := s.read(id)
		if err == nil {
			a, _ := json.Marshal(previous)
			b, _ := json.Marshal(entry)
			if string(a) == string(b) {
				continue
			}
		}
		if _, err = s.save(entry, false); err != nil {
			return err
		}
	}
	return nil
}

// Merge commits validated remote operations before updating the archive.
// Invalid records are isolated; valid records are still applied.
func (s *Store) Merge(remote []Operation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.syncReady {
		return errors.New("sync is not enabled")
	}
	var failures []error
	for _, op := range remote {
		if err := op.Validate(); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := s.writeOperation(op); err != nil {
			failures = append(failures, err)
		}
	}
	failures = append(failures, s.materialize())
	return errors.Join(failures...)
}

// Pending is scoped by destination, so switching accounts never reuses the old
// account's upload acknowledgements.
func (s *Store) Pending(destination string) ([]Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ops, err := s.operations()
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(destination))
	ack := filepath.Join(s.syncDir(), "ack", hex.EncodeToString(digest[:]))
	var pending []Operation
	for _, op := range ops {
		if _, err := os.Stat(filepath.Join(ack, op.ID)); errors.Is(err, os.ErrNotExist) {
			pending = append(pending, op)
		} else if err != nil {
			return nil, err
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].ID < pending[j].ID })
	return pending, nil
}

func (s *Store) Acknowledge(destination, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !validID(id) {
		return errors.New("invalid operation ID")
	}
	digest := sha256.Sum256([]byte(destination))
	dir := filepath.Join(s.syncDir(), "ack", hex.EncodeToString(digest[:]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(dir, id), nil)
}
