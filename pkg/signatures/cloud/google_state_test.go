package cloud

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"emanuelelongo/proview/pkg/signatures"
)

type stateTestFile struct {
	meta    driveFile
	data    []byte
	version int
}
type stateDriveServer struct {
	lostIndexResponse                       bool
	mu                                      sync.Mutex
	files                                   map[string]*stateTestFile
	next, requests, payloadReads, conflicts int
	failPublish, failTrash                  bool
	race                                    func(*stateTestFile)
}

func (s *stateDriveServer) add(name string, data []byte) *stateTestFile {
	s.next++
	id := fmt.Sprintf("file-%d", s.next)
	file := &stateTestFile{meta: driveFile{ID: id, Name: name, MIME: "application/json", Size: strconv.Itoa(len(data)), Parents: []string{"signatures"}}, data: data, version: 1}
	hash := md5.Sum(data)
	file.meta.Checksum = hex.EncodeToString(hash[:])
	s.files[id] = file
	return file
}
func (s *stateDriveServer) set(f *stateTestFile, data []byte) {
	f.data = data
	f.version++
	f.meta.Size = strconv.Itoa(len(data))
	hash := md5.Sum(data)
	f.meta.Checksum = hex.EncodeToString(hash[:])
}
func (s *stateDriveServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	w.Header().Set("Content-Type", "application/json")
	if r.Method == "POST" {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		reader := multipart.NewReader(r.Body, params["boundary"])
		part, err := reader.NextPart()
		if err != nil {
			http.Error(w, "multipart", 400)
			return
		}
		var meta driveFile
		if json.NewDecoder(part).Decode(&meta) != nil {
			http.Error(w, "metadata", 400)
			return
		}
		part, err = reader.NextPart()
		if err != nil {
			http.Error(w, "content", 400)
			return
		}
		data, _ := io.ReadAll(part)
		if meta.Name == driveIndexName && s.failPublish {
			http.Error(w, "publish failed", 500)
			return
		}
		file := s.add(meta.Name, data)
		if meta.Name == driveIndexName && s.lostIndexResponse {
			s.lostIndexResponse = false
			http.Error(w, "lost response", 500)
			return
		}
		json.NewEncoder(w).Encode(file.meta)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/files") {
		q := r.URL.Query().Get("q")
		if strings.Contains(q, "name = 'proview'") {
			json.NewEncoder(w).Encode(map[string]any{"files": []driveFile{{ID: "root", Name: "proview", MIME: folderMIME}}})
			return
		}
		if strings.Contains(q, "name = 'signatures'") {
			json.NewEncoder(w).Encode(map[string]any{"files": []driveFile{{ID: "signatures", Name: "signatures", MIME: folderMIME}}})
			return
		}
		var files []driveFile
		for _, file := range s.files {
			if !file.meta.Trashed {
				files = append(files, file.meta)
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"files": files})
		return
	}
	id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	if id == "root" {
		json.NewEncoder(w).Encode(driveFile{ID: "root", Name: "proview", MIME: folderMIME})
		return
	}
	file := s.files[id]
	if file == nil || file.meta.Trashed {
		http.NotFound(w, r)
		return
	}
	etag := fmt.Sprintf("\"revision-%d\"", file.version)
	if r.Method == "PUT" {
		if s.failPublish {
			http.Error(w, "publish failed", 500)
			return
		}
		if s.race != nil {
			hook := s.race
			s.race = nil
			hook(file)
			etag = fmt.Sprintf("\"revision-%d\"", file.version)
		}
		if r.Header.Get("If-Match") != etag {
			s.conflicts++
			w.WriteHeader(412)
			return
		}
		if r.URL.Query().Get("newRevision") != "false" {
			http.Error(w, "revision retention", 400)
			return
		}
		data, _ := io.ReadAll(r.Body)
		s.set(file, data)
		fmt.Fprint(w, `{}`)
		return
	}
	if r.Method == "PATCH" {
		if s.failTrash {
			http.Error(w, "trash failed", 500)
			return
		}
		file.meta.Trashed = true
		fmt.Fprint(w, `{}`)
		return
	}
	if r.URL.Query().Get("alt") == "media" {
		// Drive media and metadata representations have different ETags.
		if tag := r.Header.Get("If-Match"); tag != "" && tag != "\"media-revision\"" {
			w.WriteHeader(412)
			return
		}
		if file.meta.Name != driveIndexName {
			s.payloadReads++
		}
		w.Write(file.data)
		return
	}
	json.NewEncoder(w).Encode(map[string]string{"etag": etag, "md5Checksum": file.meta.Checksum})
}
func newStateDrive(t *testing.T) (*stateDriveServer, *GoogleDrive) {
	t.Helper()
	fake := &stateDriveServer{files: map[string]*stateTestFile{}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	drive := &GoogleDrive{Client: server.Client(), BaseURL: server.URL + "/drive/v3", FolderID: "root", folders: []string{"signatures"}, preparedAt: time.Now()}
	return fake, drive
}
func currentEntry(t *testing.T, store *signatures.Store, name string) signatures.Entry {
	t.Helper()
	a, err := signatures.Import("s.svg", []byte(fmt.Sprintf(`<svg viewBox="0 0 10 10"><path d="M1 1L%d 9" stroke="black"/></svg>`, len(name)+2)))
	if err != nil {
		t.Fatal(err)
	}
	e, err := store.Save(signatures.Entry{Asset: a})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func latestIndex(t *testing.T, fake *stateDriveServer) currentStateIndex {
	t.Helper()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, f := range fake.files {
		if f.meta.Name == driveIndexName && !f.meta.Trashed {
			var index currentStateIndex
			if err := json.Unmarshal(f.data, &index); err != nil {
				t.Fatal(err)
			}
			return index
		}
	}
	t.Fatal("missing current index")
	return currentStateIndex{}
}
func TestDriveCurrentMigratesHistoryAndSkipsUnchangedPayloads(t *testing.T) {
	fake, drive := newStateDrive(t)
	store := &signatures.Store{Root: t.TempDir()}
	e := currentEntry(t, store, "One")
	if err := store.EnableSync(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		var err error
		e.Asset, err = e.Asset.WithColor(color.NRGBA{R: uint8(20 + i), A: 255})
		e, err = store.Save(e)
		if err != nil {
			t.Fatal(err)
		}
	}
	ops, _ := store.Operations()
	for _, op := range ops {
		fake.add(op.ID+".json", mustJSON(op))
	}
	if err := drive.SyncCurrent(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	index := latestIndex(t, fake)
	if len(index.Records) != 1 {
		t.Fatalf("retained %d versions", len(index.Records))
	}
	for _, f := range fake.files {
		if !strings.HasPrefix(f.meta.Name, "current-") && !f.meta.Trashed {
			t.Fatal("legacy payload not retired")
		}
	}
	if err := drive.SyncCurrent(context.Background(), store); err != nil {
		t.Fatal(err)
	} // Warm index cache.
	before, reads := fake.requests, fake.payloadReads
	if err := drive.SyncCurrent(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	if fake.requests-before != 1 || fake.payloadReads != reads {
		t.Fatalf("unchanged sync: %d requests, %d payload reads", fake.requests-before, fake.payloadReads-reads)
	}
	restarted := &signatures.Store{Root: store.Root}
	if err := restarted.EnableSync(); err != nil {
		t.Fatal(err)
	}
	drive.stateCache = nil
	reads = fake.payloadReads
	if err := drive.SyncCurrent(context.Background(), restarted); err != nil {
		t.Fatal(err)
	}
	if fake.payloadReads != reads {
		t.Fatal("restart redownloaded known signatures")
	}
	pending, _ := store.Pending(drive.Destination())
	if len(pending) != 0 {
		t.Fatal("superseded operations remain pending")
	}
}
func TestDriveCurrentDeletionDoesNotResurrectOfflineCopy(t *testing.T) {
	fake, drive := newStateDrive(t)
	a := &signatures.Store{Root: t.TempDir()}
	e := currentEntry(t, a, "One")
	a.EnableSync()
	if err := drive.SyncCurrent(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	b := &signatures.Store{Root: t.TempDir()}
	b.EnableSync()
	other := &GoogleDrive{Client: drive.Client, BaseURL: drive.BaseURL, FolderID: "root", folders: drive.folders}
	if err := other.SyncCurrent(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	if err := a.Delete(e.ID); err != nil {
		t.Fatal(err)
	}
	if err := drive.SyncCurrent(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if err := other.SyncCurrent(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	entries, _ := b.List()
	if len(entries) != 0 {
		t.Fatal("offline copy restored a deleted signature")
	}
	index := latestIndex(t, fake)
	if len(index.Records) != 1 || !index.Records[0].Operation.Deleted || index.Records[0].FileID != "" {
		t.Fatal("deletion retained an asset instead of a tombstone")
	}
}
func TestDriveCurrentRetriesConcurrentWriterWithoutLostChanges(t *testing.T) {
	fake, drive := newStateDrive(t)
	a := &signatures.Store{Root: t.TempDir()}
	e := currentEntry(t, a, "One")
	a.EnableSync()
	if err := drive.SyncCurrent(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	e.Asset, _ = e.Asset.WithThickness(2)
	if _, err := a.Save(e); err != nil {
		t.Fatal(err)
	}
	b := &signatures.Store{Root: t.TempDir()}
	currentEntry(t, b, "Second signature")
	b.EnableSync()
	state, _ := b.CurrentSyncState()
	remote := state.Operations[0]
	payload := fake.add("current-"+remote.ID+".json", mustJSON(remote))
	fake.race = func(file *stateTestFile) {
		var index currentStateIndex
		json.Unmarshal(file.data, &index)
		header := remote
		header.Entry = nil
		index.Records = append(index.Records, currentStateRecord{Operation: header, FileID: payload.meta.ID, Hash: operationHash(remote)})
		fake.set(file, mustJSON(index))
	}
	if err := drive.SyncCurrent(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	if fake.conflicts != 1 || len(latestIndex(t, fake).Records) != 2 {
		t.Fatal("concurrent index update lost a signature")
	}
	entries, _ := a.List()
	if len(entries) != 2 {
		t.Fatal("concurrent signature not merged locally")
	}
}
func TestDriveCurrentFailedPublicationKeepsLegacyRecoverable(t *testing.T) {
	fake, drive := newStateDrive(t)
	store := &signatures.Store{Root: t.TempDir()}
	currentEntry(t, store, "One")
	store.EnableSync()
	ops, _ := store.Operations()
	legacy := fake.add(ops[0].ID+".json", mustJSON(ops[0]))
	fake.failPublish = true
	if err := drive.SyncCurrent(context.Background(), store); err == nil {
		t.Fatal("publication failure was ignored")
	}
	if legacy.meta.Trashed {
		t.Fatal("legacy archive retired before commit")
	}
	fake.failPublish = false
	if err := drive.SyncCurrent(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	if !legacy.meta.Trashed {
		t.Fatal("migration retry did not retire legacy archive")
	}
}
func TestDriveCurrentCorruptIndexFailsClosed(t *testing.T) {
	fake, drive := newStateDrive(t)
	store := &signatures.Store{Root: t.TempDir()}
	currentEntry(t, store, "One")
	store.EnableSync()
	fake.add(driveIndexName, []byte(`{"version":99}`))
	before := len(fake.files)
	if err := drive.SyncCurrent(context.Background(), store); err == nil {
		t.Fatal("invalid index was overwritten")
	}
	if len(fake.files) != before {
		t.Fatal("invalid index caused new remote writes")
	}
}

func TestDriveCurrentRetriesInterruptedRetirement(t *testing.T) {
	fake, drive := newStateDrive(t)
	store := &signatures.Store{Root: t.TempDir()}
	currentEntry(t, store, "One")
	store.EnableSync()
	ops, _ := store.Operations()
	old := fake.add(ops[0].ID+".json", mustJSON(ops[0]))
	fake.failTrash = true
	if err := drive.SyncCurrent(context.Background(), store); err == nil {
		t.Fatal("retirement failure was ignored")
	}
	if len(latestIndex(t, fake).Records) != 1 || old.meta.Trashed {
		t.Fatal("failed retirement damaged committed state")
	}
	fake.failTrash = false
	if err := drive.SyncCurrent(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	if !old.meta.Trashed {
		t.Fatal("old payload was not retired on retry")
	}
}
func TestCoordinatorUsesCurrentStateForDownloadsAndPushes(t *testing.T) {
	fake, drive := newStateDrive(t)
	store := &signatures.Store{Root: t.TempDir()}
	entry := currentEntry(t, store, "One")
	c := &Coordinator{Store: store}
	c.Connect(drive)
	c.Open(context.Background())
	waitIdle(t, c)
	if err := c.Status().Err; err != nil {
		t.Fatal(err)
	}
	entry.Asset, _ = entry.Asset.WithThickness(2)
	if _, err := store.Save(entry); err != nil {
		t.Fatal(err)
	}
	c.Push(context.Background())
	waitIdle(t, c)
	if err := c.Status().Err; err != nil {
		t.Fatal(err)
	}
	index := latestIndex(t, fake)
	if len(index.Records) != 1 || c.Status().Pending != 0 {
		t.Fatal("push created history instead of replacing the current signature")
	}
	for _, f := range fake.files {
		if !f.meta.Trashed && f.meta.Name != driveIndexName && !strings.HasPrefix(f.meta.Name, "current-") {
			t.Fatal("coordinator used the legacy uploader")
		}
	}
}

func TestDriveCurrentRecoversLostIndexCreationResponse(t *testing.T) {
	fake, drive := newStateDrive(t)
	store := &signatures.Store{Root: t.TempDir()}
	currentEntry(t, store, "One")
	store.EnableSync()
	ops, _ := store.Operations()
	old := fake.add(ops[0].ID+".json", mustJSON(ops[0]))
	fake.lostIndexResponse = true
	if err := drive.SyncCurrent(context.Background(), store); err == nil {
		t.Fatal("lost publication response was not reported")
	}
	if old.meta.Trashed {
		t.Fatal("unconfirmed index creation retired old data")
	}
	if err := drive.SyncCurrent(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	indexes := 0
	for _, f := range fake.files {
		if f.meta.Name == driveIndexName && !f.meta.Trashed {
			indexes++
		}
	}
	if indexes != 1 || !old.meta.Trashed {
		t.Fatal("retry duplicated the index or failed to complete migration")
	}
}
