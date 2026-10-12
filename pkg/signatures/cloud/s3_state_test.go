package cloud

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"emanuelelongo/proview/pkg/signatures"
)

type stateS3Server struct {
	mu                                              sync.Mutex
	objects                                         map[string][]byte
	requests, lists, payloadReads, puts, conflicts  int
	failPublish, hidePublication, rejectConditional bool
	race                                            func()
}

func s3StateETag(data []byte) string {
	hash := md5.Sum(data)
	return `"` + hex.EncodeToString(hash[:]) + `"`
}

func (s *stateS3Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	key := strings.TrimPrefix(r.URL.Path, "/signatures/")
	if r.URL.Query().Get("list-type") == "2" {
		s.lists++
		type object struct {
			Key  string
			Size int
			ETag string
		}
		type page struct {
			XMLName     xml.Name `xml:"ListBucketResult"`
			IsTruncated bool
			Contents    []object
		}
		result := page{}
		for key, data := range s.objects {
			result.Contents = append(result.Contents, object{key, len(data), s3StateETag(data)})
		}
		xml.NewEncoder(w).Encode(result)
		return
	}
	data, exists := s.objects[key]
	switch r.Method {
	case "GET":
		if !exists {
			s3TestError(w, 404, "NoSuchKey")
			return
		}
		etag := s3StateETag(data)
		w.Header().Set("ETag", etag)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(304)
			return
		}
		if !strings.HasSuffix(key, s3IndexName) {
			s.payloadReads++
		}
		w.Write(data)
	case "PUT":
		if s.rejectConditional {
			s3TestError(w, 501, "NotImplemented")
			return
		}
		if strings.HasSuffix(key, s3IndexName) && s.race != nil {
			race := s.race
			s.race = nil
			race()
			data, exists = s.objects[key]
		}
		condition := r.Header.Get("If-Match")
		if r.Header.Get("If-None-Match") == "*" {
			if exists {
				s.conflicts++
				s3TestError(w, 412, "PreconditionFailed")
				return
			}
		} else if condition == "" || !exists || condition != s3StateETag(data) {
			s.conflicts++
			s3TestError(w, 412, "PreconditionFailed")
			return
		}
		if strings.HasSuffix(key, s3IndexName) && s.failPublish {
			s3TestError(w, 403, "AccessDenied")
			return
		}
		data, _ = io.ReadAll(r.Body)
		s.objects[key] = data
		s.puts++
		if strings.HasSuffix(key, s3IndexName) && s.hidePublication {
			s.hidePublication = false
			s3TestError(w, 403, "AccessDenied")
			return
		}
		w.Header().Set("ETag", s3StateETag(data))
	default:
		w.WriteHeader(405)
	}
}

func newStateS3(t *testing.T) (*stateS3Server, *S3) {
	t.Helper()
	fake := &stateS3Server{objects: map[string][]byte{}}
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	return fake, s3TestProvider(t, server.URL)
}
func stateS3Sync(t *testing.T, p *S3, store *signatures.Store) {
	t.Helper()
	if err := store.EnableSync(); err != nil {
		t.Fatal(err)
	}
	if err := p.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := p.SyncCurrent(context.Background(), store); err != nil {
		t.Fatal(err)
	}
}
func readS3TestIndex(t *testing.T, fake *stateS3Server, p *S3) currentStateIndex {
	t.Helper()
	fake.mu.Lock()
	defer fake.mu.Unlock()
	var index currentStateIndex
	if err := json.Unmarshal(fake.objects[p.prefix()+s3IndexName], &index); err != nil {
		t.Fatal(err)
	}
	return index
}

func TestS3CurrentMigrationAndUnchangedRestart(t *testing.T) {
	fake, p := newStateS3(t)
	op := s3TestOperation(t)
	store := &signatures.Store{Root: t.TempDir()}
	if err := store.EnableSync(); err != nil {
		t.Fatal(err)
	}
	var journal []signatures.Operation
	for n := 0; n < 16; n++ {
		revision := op
		revision.ID = fmt.Sprintf("%032x", n+1)
		revision.Updated = op.Updated.Add(time.Duration(n) * time.Second)
		entry := *op.Entry
		entry.Updated = revision.Updated
		revision.Entry = &entry
		fake.objects[p.prefix()+revision.ID+".json"] = mustJSON(revision)
		journal = append(journal, revision)
	}
	if err := store.Merge(journal); err != nil {
		t.Fatal(err)
	}
	stateS3Sync(t, p, store)
	if index := readS3TestIndex(t, fake, p); len(index.Records) != 1 || index.Records[0].Operation.ID != journal[15].ID {
		t.Fatal("migration did not compact history")
	}
	if fake.payloadReads != 0 {
		t.Fatal("known migration payloads were downloaded")
	}
	before, puts, lists := fake.requests, fake.puts, fake.lists
	stateS3Sync(t, p, store)
	if fake.requests-before != 1 || fake.puts != puts || fake.lists != lists || fake.payloadReads != 0 {
		t.Fatal("unchanged state did more than one conditional index check")
	}
	pending, err := store.Pending(p.Destination())
	if err != nil || len(pending) != 0 {
		t.Fatal("migration did not acknowledge old revisions")
	}
	restarted := &signatures.Store{Root: store.Root}
	other := s3TestProvider(t, p.Config.Endpoint)
	stateS3Sync(t, other, restarted)
	if fake.payloadReads != 0 {
		t.Fatal("restart downloaded known payloads")
	}
	fresh := &signatures.Store{Root: t.TempDir()}
	stateS3Sync(t, s3TestProvider(t, p.Config.Endpoint), fresh)
	entries, err := fresh.List()
	if err != nil || len(entries) != 1 || fake.payloadReads != 1 {
		t.Fatal("fresh installation did not read only current payload")
	}
}

func TestS3CurrentDeletionSurvivesOfflineCopy(t *testing.T) {
	fake, p := newStateS3(t)
	store := &signatures.Store{Root: t.TempDir()}
	entry := currentEntry(t, store, "First")
	stateS3Sync(t, p, store)
	offline := &signatures.Store{Root: t.TempDir()}
	other := s3TestProvider(t, p.Config.Endpoint)
	stateS3Sync(t, other, offline)
	if err := store.Delete(entry.ID); err != nil {
		t.Fatal(err)
	}
	stateS3Sync(t, p, store)
	stateS3Sync(t, other, offline)
	entries, err := offline.List()
	if err != nil || len(entries) != 0 {
		t.Fatal("offline copy resurrected deletion")
	}
	index := readS3TestIndex(t, fake, p)
	if len(index.Records) != 1 || !index.Records[0].Operation.Deleted || index.Records[0].FileID != "" {
		t.Fatal("deletion retained payload in current index")
	}
}

func TestS3CurrentDownloadsOnlyChangedSignature(t *testing.T) {
	fake, writer := newStateS3(t)
	store := &signatures.Store{Root: t.TempDir()}
	entry := currentEntry(t, store, "First")
	currentEntry(t, store, "Another")
	stateS3Sync(t, writer, store)
	reader := s3TestProvider(t, writer.Config.Endpoint)
	other := &signatures.Store{Root: t.TempDir()}
	stateS3Sync(t, reader, other)
	before := fake.payloadReads
	updated, err := entry.Asset.WithThickness(3)
	if err != nil {
		t.Fatal(err)
	}
	entry.Asset = updated
	if _, err := store.Save(entry); err != nil {
		t.Fatal(err)
	}
	stateS3Sync(t, writer, store)
	stateS3Sync(t, reader, other)
	if fake.payloadReads-before != 1 {
		t.Fatal("unchanged signatures were downloaded")
	}
	entries, err := other.List()
	if err != nil || len(entries) != 2 {
		t.Fatal("changed state lost another signature")
	}
	found := false
	for _, entry := range entries {
		found = found || entry.Asset.Thickness == 3
	}
	if !found {
		t.Fatal("changed signature was not merged")
	}
}

func TestS3CurrentConcurrentUpdateMergesAndRetries(t *testing.T) {
	fake, p := newStateS3(t)
	store := &signatures.Store{Root: t.TempDir()}
	currentEntry(t, store, "First")
	stateS3Sync(t, p, store)
	currentEntry(t, store, "Second local")
	remote := s3TestOperation(t)
	header := remote
	header.Entry = nil
	fake.race = func() {
		var index currentStateIndex
		json.Unmarshal(fake.objects[p.prefix()+s3IndexName], &index)
		index.Records = append(index.Records, currentStateRecord{Operation: header, FileID: p.payloadKey(remote.ID), Hash: operationHash(remote)})
		fake.objects[p.payloadKey(remote.ID)] = mustJSON(remote)
		fake.objects[p.prefix()+s3IndexName] = mustJSON(index)
	}
	stateS3Sync(t, p, store)
	if fake.conflicts < 1 || len(readS3TestIndex(t, fake, p).Records) != 3 {
		t.Fatal("concurrent writer was overwritten")
	}
	entries, err := store.List()
	if err != nil || len(entries) != 3 {
		t.Fatal("remote race did not merge locally")
	}
}

func TestS3CurrentFailedPublicationAndLostResponse(t *testing.T) {
	for _, hidden := range []bool{false, true} {
		t.Run(fmt.Sprint(hidden), func(t *testing.T) {
			fake, p := newStateS3(t)
			store := &signatures.Store{Root: t.TempDir()}
			currentEntry(t, store, "First")
			if err := store.EnableSync(); err != nil {
				t.Fatal(err)
			}
			fake.failPublish = !hidden
			fake.hidePublication = hidden
			if err := p.SyncCurrent(context.Background(), store); err == nil {
				t.Fatal("publication failure was hidden")
			}
			pending, _ := store.Pending(p.Destination())
			if len(pending) == 0 {
				t.Fatal("failed publication acknowledged operations")
			}
			fake.failPublish = false
			stateS3Sync(t, p, store)
			if len(readS3TestIndex(t, fake, p).Records) != 1 || len(fake.objects) != 2 {
				t.Fatal("retry duplicated or lost current payload")
			}
		})
	}
}

func TestS3CurrentRejectsInvalidIndexAndUnsupportedConditions(t *testing.T) {
	for _, scenario := range []string{"corrupt", "outside-prefix", "unsupported"} {
		t.Run(scenario, func(t *testing.T) {
			fake, p := newStateS3(t)
			store := &signatures.Store{Root: t.TempDir()}
			currentEntry(t, store, "First")
			store.EnableSync()
			switch scenario {
			case "corrupt":
				fake.objects[p.prefix()+s3IndexName] = []byte(`{"version":2,"records":[{}]}`)
			case "outside-prefix":
				op := s3TestOperation(t)
				header := op
				header.Entry = nil
				fake.objects[p.prefix()+s3IndexName] = mustJSON(currentStateIndex{Version: 2, Records: []currentStateRecord{{Operation: header, FileID: "other/private.json", Hash: operationHash(op)}}})
			case "unsupported":
				fake.rejectConditional = true
			}
			err := p.SyncCurrent(context.Background(), store)
			if err == nil || fake.puts != 0 {
				t.Fatal("invalid or unsafe remote state was accepted")
			}
			for _, secret := range []string{"test-secret", "test-access", p.Config.Endpoint} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("error exposed connection details")
				}
			}
		})
	}
}
