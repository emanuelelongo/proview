package signatures

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type Entry struct {
	Version    int       `json:"version"`
	ID         string    `json:"id"`
	Created    time.Time `json:"created"`
	Updated    time.Time `json:"updated"`
	Asset      Asset     `json:"asset"`
	legacyName string
}

type Store struct {
	Root        string
	mu          sync.Mutex
	syncReady   bool
	syncAliases map[string]string
}

func DataDir(goos, home string, getenv func(string) string) (string, error) {
	var root string
	switch goos {
	case "darwin":
		root = filepath.Join(home, "Library", "Application Support", "Proview")
	case "windows":
		root = getenv("LOCALAPPDATA")
		if root == "" || !filepath.IsAbs(root) {
			root = filepath.Join(home, "AppData", "Local")
		}
		root = filepath.Join(root, "Proview")
	default:
		root = getenv("XDG_DATA_HOME")
		if root == "" || !filepath.IsAbs(root) {
			root = filepath.Join(home, ".local", "share")
		}
		root = filepath.Join(root, "proview")
	}
	if home == "" && !filepath.IsAbs(root) {
		return "", errors.New("cannot locate the application data directory")
	}
	return filepath.Join(root, "signatures"), nil
}

func DefaultStore() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root, err := DataDir(runtime.GOOS, home, os.Getenv)
	if err != nil {
		return nil, err
	}
	return &Store{Root: root}, nil
}

func newID() (string, error) {
	var id [16]byte
	_, err := rand.Read(id[:])
	return hex.EncodeToString(id[:]), err
}
func validID(id string) bool {
	data, err := hex.DecodeString(id)
	return err == nil && len(data) == 16 && id == strings.ToLower(id)
}

func (s *Store) List() ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverSync(); err != nil {
		return nil, err
	}
	return s.list()
}

func (s *Store) list() ([]Entry, error) {
	dirs, err := os.ReadDir(s.Root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []Entry
	var failures []error
	for _, dir := range dirs {
		if !dir.IsDir() || !validID(dir.Name()) {
			continue
		}
		if _, err := os.Stat(filepath.Join(s.Root, dir.Name(), "current.json")); errors.Is(err, os.ErrNotExist) {
			continue
		}
		entry, err := s.read(dir.Name())
		if err != nil {
			failures = append(failures, fmt.Errorf("signature %s: %w", dir.Name(), err))
			continue
		}
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Created.Equal(entries[j].Created) {
			return entries[i].ID < entries[j].ID
		}
		return entries[i].Created.Before(entries[j].Created)
	})
	return entries, errors.Join(failures...)
}

func (s *Store) read(id string) (Entry, error) {
	var entry Entry
	if !validID(id) {
		return entry, errors.New("invalid signature ID")
	}
	data, err := os.ReadFile(filepath.Join(s.Root, id, "current.json"))
	if err != nil {
		return entry, err
	}
	var manifest struct {
		Revision string `json:"revision"`
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		return entry, err
	}
	if !validID(manifest.Revision) {
		return entry, errors.New("invalid signature revision")
	}
	path := filepath.Join(s.Root, id, manifest.Revision, "metadata.json")
	info, err := os.Stat(path)
	if err != nil {
		return entry, err
	}
	if info.Size() > MaxFileSize*2 {
		return entry, errors.New("signature metadata is too large")
	}
	data, err = os.ReadFile(path)
	if err != nil {
		return entry, err
	}
	if err = json.Unmarshal(data, &entry); err != nil {
		return entry, err
	}
	if entry.Version != 1 || entry.ID != id {
		return entry, errors.New("invalid signature metadata")
	}
	if err = entry.Asset.Validate(); err != nil {
		return entry, err
	}
	return entry, nil
}

func (s *Store) Save(entry Entry) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverSync(); err != nil {
		return Entry{}, err
	}
	saved, err := s.save(entry, true)
	if err != nil || !s.syncReady {
		return saved, err
	}
	if err = s.materialize(); err != nil {
		return Entry{}, err
	}
	ops, err := s.operations()
	if err != nil {
		return Entry{}, err
	}
	_, aliases, ids := resolveWithAliases(ops, s.syncAliases)
	identity := s.identityFor(saved.ID, ops)
	return s.read(ids[aliases[identity]])
}

func (s *Store) save(entry Entry, local bool) (Entry, error) {
	if local {
		entry.legacyName = ""
	}
	if err := entry.Asset.Validate(); err != nil {
		return Entry{}, err
	}
	now := time.Now().UTC()
	if entry.ID == "" {
		id, err := newID()
		if err != nil {
			return Entry{}, err
		}
		entry.ID = id
		entry.Created = now
	} else if local {
		previous, err := s.read(entry.ID)
		if err != nil {
			return Entry{}, err
		}
		entry.Created = previous.Created
	}
	entry.Version = 1
	if local {
		entry.Updated = now
	}
	if local && s.syncReady {
		if err := s.recordSave(entry); err != nil {
			return Entry{}, err
		}
	}
	revision, err := newID()
	if err != nil {
		return Entry{}, err
	}
	directory := filepath.Join(s.Root, entry.ID, revision)
	if err = os.MkdirAll(directory, 0700); err != nil {
		return Entry{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(directory)
		}
	}()
	metadata, err := json.Marshal(entry)
	if err != nil {
		return Entry{}, err
	}
	if err = writeFile(filepath.Join(directory, "metadata.json"), metadata); err != nil {
		return Entry{}, err
	}
	extension := "svg"
	if entry.Asset.Kind == PNG {
		extension = "png"
	}
	if err = writeFile(filepath.Join(directory, "signature."+extension), entry.Asset.Data); err != nil {
		return Entry{}, err
	}
	manifest, _ := json.Marshal(struct {
		Revision string `json:"revision"`
	}{revision})
	if err = writeAtomic(filepath.Join(s.Root, entry.ID, "current.json"), manifest); err != nil {
		return Entry{}, err
	}
	committed = true
	// Previous complete revisions and interrupted saves are disposable after commit.
	dirs, _ := os.ReadDir(filepath.Join(s.Root, entry.ID))
	for _, dir := range dirs {
		if dir.IsDir() && validID(dir.Name()) && dir.Name() != revision {
			_ = os.RemoveAll(filepath.Join(s.Root, entry.ID, dir.Name()))
		}
	}
	entry.Asset = entry.Asset.Clone()
	return entry, nil
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.recoverSync(); err != nil {
		return err
	}
	if s.syncReady {
		if err := s.recordDelete(id); err != nil {
			return err
		}
	}
	return s.delete(id)
}

func (s *Store) delete(id string) error {
	if !validID(id) {
		return errors.New("invalid signature ID")
	}
	tombstone, err := newID()
	if err != nil {
		return err
	}
	deleted := filepath.Join(s.Root, ".deleted-"+tombstone)
	if err = os.Rename(filepath.Join(s.Root, id), deleted); err != nil {
		return err
	}
	return os.RemoveAll(deleted)
}

func writeFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	err = errors.Join(err, f.Sync(), f.Close())
	return err
}

func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".signature-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	_, err = f.Write(data)
	err = errors.Join(err, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	return os.Rename(name, path)
}
