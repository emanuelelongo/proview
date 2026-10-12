package cloud

import (
	"bytes"
	"context"
	"emanuelelongo/proview/pkg/signatures"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGoogleDriveCreatesFoldersUploadsAndRecoversLostResponse(t *testing.T) {
	for _, name := range []string{"proview", "Firme d'ufficio 日本語"} {
		t.Run(name, func(t *testing.T) { testGoogleDriveFolderUploadsAndRetries(t, name) })
	}
}

func testGoogleDriveFolderUploadsAndRetries(t *testing.T, name string) {
	t.Helper()
	store := &signatures.Store{Root: t.TempDir()}
	save(t, store, "Test")
	if err := store.EnableSync(); err != nil {
		t.Fatal(err)
	}
	ops, _ := store.Operations()
	op := ops[0]
	root, child := false, false
	uploads := 0
	var uploaded []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "POST" && r.URL.Path == "/upload/drive/v3/files" {
			media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if err != nil || media != "multipart/related" {
				t.Errorf("wrong upload type: %v", err)
				w.WriteHeader(400)
				return
			}
			reader := multipart.NewReader(r.Body, params["boundary"])
			metadata, err := reader.NextPart()
			if err != nil {
				t.Fatal(err)
			}
			var file driveFile
			if err = json.NewDecoder(metadata).Decode(&file); err != nil {
				t.Fatal(err)
			}
			if file.Name != op.ID+".json" || len(file.Parents) != 1 || file.Parents[0] != "signatures-id" {
				t.Errorf("wrong upload metadata: %+v", file)
			}
			part, err := reader.NextPart()
			if err != nil {
				t.Fatal(err)
			}
			uploaded, err = io.ReadAll(part)
			if err != nil {
				t.Fatal(err)
			}
			uploads++
			// The server committed the file, but the caller could not observe success.
			w.WriteHeader(500)
			return
		}
		if r.Method == "POST" {
			var fields map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
				t.Fatal(err)
			}
			if _, ok := fields["size"]; ok {
				t.Error("sent read-only size field")
			}
			if _, ok := fields["id"]; ok {
				t.Error("sent empty file ID")
			}
			var createdName string
			json.Unmarshal(fields["name"], &createdName)
			if createdName == name {
				root = true
				fmt.Fprint(w, `{"id":"root-id"}`)
			} else if createdName == "signatures" {
				child = true
				fmt.Fprint(w, `{"id":"signatures-id"}`)
			} else {
				t.Errorf("unexpected folder %s", name)
			}
			return
		}
		if r.URL.Query().Get("alt") == "media" {
			w.Write(uploaded)
			return
		}
		if r.URL.Path == "/drive/v3/files/root-id" {
			json.NewEncoder(w).Encode(driveFile{ID: "root-id", Name: name, MIME: folderMIME})
			return
		}
		q := r.URL.Query().Get("q")
		var files []driveFile
		switch {
		case strings.Contains(q, "name = '"+escapeQuery(name)+"'"):
			if root {
				files = []driveFile{{ID: "root-id", Name: name, MIME: folderMIME}}
			}
		case strings.Contains(q, "name = 'signatures'"):
			if child {
				files = []driveFile{{ID: "signatures-id", Name: "signatures", MIME: folderMIME}}
			}
		case strings.Contains(q, "'signatures-id' in parents"):
			if uploaded != nil {
				files = []driveFile{{ID: "operation-file", Name: op.ID + ".json", MIME: "application/json", Size: fmt.Sprint(len(uploaded))}}
			}
		default:
			t.Errorf("unexpected Drive query: %s", q)
		}
		json.NewEncoder(w).Encode(map[string]any{"files": files})
	}))
	defer server.Close()
	selected := ""
	drive := &GoogleDrive{Client: server.Client(), FolderName: name, BaseURL: server.URL + "/drive/v3", OnFolder: func(id string) { selected = id }}
	ctx := context.Background()
	if err := drive.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	if selected != "root-id" || !root || !child {
		t.Fatal("folders were not initialized")
	}
	if err := drive.Upload(ctx, op); err == nil {
		t.Fatal("lost upload response not reported")
	}
	if err := drive.Upload(ctx, op); err != nil {
		t.Fatal(err)
	}
	if uploads != 1 {
		t.Fatalf("upload retry created %d copies", uploads)
	}
	if !bytes.Equal(uploaded, mustJSON(op)) {
		t.Fatal("signature changed during upload")
	}
	if err := drive.Prepare(ctx); err != nil {
		t.Fatal(err)
	}
	downloaded, err := drive.Download(ctx)
	if err != nil || len(downloaded) != 1 {
		t.Fatalf("download=%d err=%v", len(downloaded), err)
	}
}
func TestGoogleDrivePaginationDuplicateFoldersAndCorruptIsolation(t *testing.T) {
	store := &signatures.Store{Root: t.TempDir()}
	save(t, store, "Remote")
	store.EnableSync()
	ops, _ := store.Operations()
	op := ops[0]
	data := mustJSON(op)
	queries := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("alt") == "media" {
			if strings.HasSuffix(r.URL.Path, "/bad") {
				fmt.Fprint(w, `{"version":77}`)
			} else {
				w.Write(data)
			}
			return
		}
		q := r.URL.Query().Get("q")
		if strings.Contains(q, "name = 'proview'") {
			queries++
			if r.URL.Query().Get("pageToken") == "" {
				fmt.Fprint(w, `{"files":[{"id":"b"}],"nextPageToken":"next"}`)
			} else {
				fmt.Fprint(w, `{"files":[{"id":"a"}]}`)
			}
			return
		}
		if strings.Contains(q, "name = 'signatures'") {
			if strings.Contains(q, "'a' in parents") {
				fmt.Fprint(w, `{"files":[{"id":"sig-a"}]}`)
			} else {
				fmt.Fprint(w, `{"files":[{"id":"sig-b"}]}`)
			}
			return
		}
		files := []driveFile{{ID: "valid", Name: op.ID + ".json", Size: fmt.Sprint(len(data))}}
		if strings.Contains(q, "'sig-b' in parents") {
			files = append(files, driveFile{ID: "bad", Name: "bad.json", Size: "14"}, driveFile{ID: "huge", Name: "huge.json", Size: "99999999999"})
		}
		json.NewEncoder(w).Encode(map[string]any{"files": files})
	}))
	defer server.Close()
	drive := &GoogleDrive{Client: server.Client(), BaseURL: server.URL + "/drive/v3"}
	if err := drive.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if drive.FolderID != "a" || queries != 2 || len(drive.folders) != 2 {
		t.Fatalf("duplicate folders not deterministic: %+v", drive)
	}
	downloaded, err := drive.Download(context.Background())
	if err == nil || len(downloaded) != 2 {
		t.Fatalf("corrupt record blocked valid operations: %d %v", len(downloaded), err)
	}
	if err = store.Merge(downloaded); err != nil {
		t.Fatal(err)
	}
	entries, _ := store.List()
	if len(entries) != 1 {
		t.Fatal("duplicate remote operations created duplicate signatures")
	}
}
func TestGoogleDriveRejectsWrongFolderAndReportsAuthorization(t *testing.T) {
	for _, test := range []struct {
		code int
		body string
	}{{200, `{"id":"x","name":"other","mimeType":"application/vnd.google-apps.folder"}`}, {200, `{"id":"x","name":"proview","mimeType":"text/plain"}`}, {200, `{"id":"x","name":"proview","mimeType":"application/vnd.google-apps.folder","trashed":true}`}, {401, `secret-token`}, {403, `secret-token`}, {404, `secret-token`}} {
		t.Run(fmt.Sprint(test.code, test.body), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(test.code); fmt.Fprint(w, test.body) }))
			defer server.Close()
			drive := &GoogleDrive{Client: server.Client(), BaseURL: server.URL}
			err := drive.SelectFolder(context.Background(), "x")
			if err == nil || strings.Contains(err.Error(), "secret-token") {
				t.Fatalf("unsafe or missing error: %v", err)
			}
		})
	}
}

func TestGoogleDriveAutoFolderConvergesButPickerChoiceIsPreserved(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/b") {
			fmt.Fprint(w, `{"id":"b","name":"proview","mimeType":"application/vnd.google-apps.folder"}`)
			return
		}
		q := r.URL.Query().Get("q")
		if strings.Contains(q, "name = 'proview'") {
			fmt.Fprint(w, `{"files":[{"id":"b"},{"id":"a"}]}`)
			return
		}
		if strings.Contains(q, "'a' in parents") {
			fmt.Fprint(w, `{"files":[{"id":"sig-a"}]}`)
		} else {
			fmt.Fprint(w, `{"files":[{"id":"sig-b"}]}`)
		}
	}))
	defer server.Close()
	for _, explicit := range []bool{false, true} {
		drive := &GoogleDrive{Client: server.Client(), BaseURL: server.URL, FolderID: "b", ExplicitFolder: explicit}
		if err := drive.Prepare(context.Background()); err != nil {
			t.Fatal(err)
		}
		want := "a"
		if explicit {
			want = "b"
		}
		if drive.FolderID != want || drive.folders[0] != "sig-"+want {
			t.Fatalf("wrong destination for explicit=%v: %+v", explicit, drive)
		}
	}
}

func TestGoogleDriveRejectsDirectoryPathsBeforeRequests(t *testing.T) {
	for _, name := range []string{"parent/child", "..", "folder\\child", "invalid\nname"} {
		drive := &GoogleDrive{FolderName: name}
		if err := drive.Prepare(context.Background()); err == nil {
			t.Fatalf("accepted invalid directory %q", name)
		}
	}
	name, err := NormalizeDriveFolderName("   ")
	if err != nil || name != "proview" {
		t.Fatal("empty directory did not use default")
	}
}
