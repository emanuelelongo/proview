package cloud

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"emanuelelongo/proview/pkg/signatures"
)

func s3TestOperation(t *testing.T) signatures.Operation {
	t.Helper()
	store := &signatures.Store{Root: t.TempDir()}
	save(t, store, "Test")
	if err := store.EnableSync(); err != nil {
		t.Fatal(err)
	}
	ops, err := store.Operations()
	if err != nil || len(ops) != 1 {
		t.Fatalf("operations: %v, %v", ops, err)
	}
	return ops[0]
}
func s3TestProvider(t *testing.T, endpoint string) *S3 {
	t.Helper()
	p, err := NewS3(S3Config{Endpoint: endpoint, Bucket: "signatures", PathStyle: true}, S3Keys{AccessKey: "test-access", SecretKey: "test-secret"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func s3TestError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	fmt.Fprintf(w, "<Error><Code>%s</Code><Message>test-secret test-access</Message></Error>", code)
}

func TestS3SignedUploadRetriesWithoutOverwriting(t *testing.T) {
	op := s3TestOperation(t)
	var stored []byte
	uploads := 0
	key := "/signatures/proview/signatures/" + op.ID + ".json"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-access/") || r.Header.Get("X-Amz-Security-Token") != "" {
			t.Error("request was not signed with the supplied credentials")
		}
		if r.Method == "GET" && r.URL.Query().Get("list-type") == "2" {
			if r.URL.Query().Get("prefix") != "proview/signatures/" || r.URL.Path != "/signatures" {
				t.Errorf("wrong destination: %s", r.URL)
			}
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
			return
		}
		if r.URL.Path != key {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(400)
			return
		}
		switch r.Method {
		case "GET":
			if stored == nil {
				s3TestError(w, 404, "NoSuchKey")
				return
			}
			w.Write(stored)
		case "PUT":
			if r.Header.Get("If-None-Match") != "*" || r.Header.Get("Content-Type") != "application/json" {
				t.Error("upload is not conditional JSON")
			}
			if stored != nil {
				t.Error("attempted to overwrite an existing operation")
				s3TestError(w, 412, "PreconditionFailed")
				return
			}
			stored, _ = io.ReadAll(r.Body)
			uploads++
			// Commit succeeded but a nonretryable failure hides its success.
			s3TestError(w, 403, "AccessDenied")
		default:
			t.Errorf("unexpected method: %s", r.Method)
			w.WriteHeader(405)
		}
	}))
	defer server.Close()
	p := s3TestProvider(t, server.URL)
	ctx := context.Background()
	if err := p.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	if err := p.Upload(ctx, op); err == nil {
		t.Fatal("hidden upload success was not reported")
	}
	if err := p.Upload(ctx, op); err != nil {
		t.Fatal(err)
	}
	if uploads != 1 || !bytes.Equal(stored, mustJSON(op)) {
		t.Fatal("retry duplicated or changed the operation")
	}
	changed := op
	entry := *op.Entry
	entry.Created = entry.Created.Add(-time.Second)
	changed.Entry = &entry
	if err := p.Upload(ctx, changed); err == nil || !strings.Contains(err.Error(), "Conflicting") {
		t.Fatalf("conflicting ID was accepted: %v", err)
	}
}

func TestS3ConcurrentConditionalUpload(t *testing.T) {
	op := s3TestOperation(t)
	reads := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			reads++
			if reads == 1 {
				s3TestError(w, 404, "NoSuchKey")
			} else {
				w.Write(mustJSON(op))
			}
			return
		}
		if r.Method == "PUT" {
			s3TestError(w, 412, "PreconditionFailed")
			return
		}
		w.WriteHeader(405)
	}))
	defer server.Close()
	if err := s3TestProvider(t, server.URL).Upload(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	if reads != 2 {
		t.Fatal("conditional conflict was not verified")
	}
}

func TestS3DownloadPaginatesAndSkipsInvalidOperations(t *testing.T) {
	op := s3TestOperation(t)
	prefix := "proview/signatures/"
	pages := 0
	type object struct {
		Key  string
		Size int64
	}
	type page struct {
		XMLName               xml.Name `xml:"ListBucketResult"`
		IsTruncated           bool
		NextContinuationToken string
		Contents              []object
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			pages++
			result := page{}
			if r.URL.Query().Get("continuation-token") == "" {
				result.IsTruncated = true
				result.NextContinuationToken = "page2"
				result.Contents = []object{{prefix + "notes.txt", 5}, {prefix + "nested/ignored.json", 5}, {prefix + "large.json", signatures.MaxOperationSize + 1}, {prefix + "bad.json", 2}}
			} else {
				if r.URL.Query().Get("continuation-token") != "page2" {
					t.Error("wrong continuation token")
				}
				result.Contents = []object{{prefix + op.ID + ".json", int64(len(mustJSON(op)))}, {prefix + "mismatch.json", int64(len(mustJSON(op)))}}
			}
			xml.NewEncoder(w).Encode(result)
			return
		}
		switch r.URL.Path {
		case "/signatures/" + prefix + "bad.json":
			fmt.Fprint(w, "{}")
		case "/signatures/" + prefix + op.ID + ".json", "/signatures/" + prefix + "mismatch.json":
			w.Write(mustJSON(op))
		default:
			t.Errorf("unexpected download: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	ops, err := s3TestProvider(t, server.URL).Download(context.Background())
	if err == nil || len(ops) != 1 || ops[0].ID != op.ID || pages != 2 {
		t.Fatalf("download: %d ops, %d pages, %v", len(ops), pages, err)
	}
}

func TestS3RejectsRepeatedPaginationAndRedactsErrors(t *testing.T) {
	for _, status := range []int{200, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if status != 200 {
					s3TestError(w, status, "AccessDenied")
					return
				}
				fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>repeat</NextContinuationToken></ListBucketResult>`)
			}))
			defer server.Close()
			_, err := s3TestProvider(t, server.URL).Download(context.Background())
			if err == nil {
				t.Fatal("invalid pagination or access denial succeeded")
			}
			for _, secret := range []string{"test-secret", "test-access", server.URL} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("error exposed connection details")
				}
			}
		})
	}
}

func TestS3ConfigurationAndDestinationIsolation(t *testing.T) {
	config := S3Config{Endpoint: "https://s3.example.com/", Bucket: "bucket", PathStyle: true}
	keys := S3Keys{AccessKey: "key", SecretKey: "secret"}
	first, err := NewS3(config, keys)
	if err != nil {
		t.Fatal(err)
	}
	if first.Config.Region != "us-east-1" || first.Config.Prefix != "proview/signatures" {
		t.Fatal("missing defaults")
	}
	for _, mutate := range []func(*S3Config){
		func(c *S3Config) { c.Bucket = "other" }, func(c *S3Config) { c.Prefix = "other" }, func(c *S3Config) { c.Endpoint = "https://other.example.com" },
	} {
		changed := config
		mutate(&changed)
		other, err := NewS3(changed, keys)
		if err != nil || first.Destination() == other.Destination() {
			t.Fatal("different archives shared acknowledgements")
		}
	}
	config.PathStyle = false
	config.Region = "auto"
	other, err := NewS3(config, S3Keys{AccessKey: "other", SecretKey: "other"})
	if err != nil || first.Destination() != other.Destination() {
		t.Fatal("same archive changed identity with credentials or addressing")
	}
	for _, endpoint := range []string{"", "file:///tmp/bucket", "https://key:secret@s3.example.com", "https://s3.example.com?secret=key", "https://s3.example.com#key"} {
		config.Endpoint = endpoint
		if _, err := NewS3(config, keys); err == nil {
			t.Fatalf("accepted endpoint %q", endpoint)
		}
	}
	config.Endpoint = "https://s3.example.com"
	config.Prefix = "../escape"
	if _, err := NewS3(config, keys); err == nil {
		t.Fatal("accepted invalid prefix")
	}
	config.Prefix = "valid"
	if _, err := NewS3(config, S3Keys{}); err == nil {
		t.Fatal("accepted empty credentials")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := first.Prepare(ctx); err != context.Canceled {
		t.Fatalf("cancellation: %v", err)
	}
}
