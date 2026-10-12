package cloud

import (
	"bytes"
	"context"
	"crypto/sha256"
	"emanuelelongo/proview/pkg/signatures"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const driveAPI = "https://www.googleapis.com/drive/v3"
const folderMIME = "application/vnd.google-apps.folder"

type GoogleDrive struct {
	Client         *http.Client
	FolderID       string
	FolderName     string
	ExplicitFolder bool
	// OnFolder persists the chosen folder without coupling the provider to Fyne.
	OnFolder   func(string)
	BaseURL    string // injectable API endpoint for tests
	folders    []string
	uploaded   map[string][32]byte
	preparedAt time.Time
	indexMu    sync.Mutex
	stateCache map[string]cachedDriveIndex
}
type driveFile struct {
	ID         string            `json:"id,omitempty"`
	Name       string            `json:"name"`
	MIME       string            `json:"mimeType"`
	Parents    []string          `json:"parents,omitempty"`
	Size       string            `json:"size,omitempty"`
	Trashed    bool              `json:"trashed,omitempty"`
	DriveID    string            `json:"driveId,omitempty"`
	Checksum   string            `json:"md5Checksum,omitempty"`
	Properties map[string]string `json:"appProperties,omitempty"`
}

// NormalizeDriveFolderName accepts one directory name in the root of My Drive.
func NormalizeDriveFolderName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "proview", nil
	}
	if name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00\r\n") || len([]rune(name)) > 255 {
		return "", errors.New("Enter a Google Drive directory name, without a path")
	}
	return name, nil
}
func (g *GoogleDrive) folderName() string {
	if g.FolderName == "" {
		return "proview"
	}
	return g.FolderName
}

func (g *GoogleDrive) base() string {
	if g.BaseURL != "" {
		return g.BaseURL
	}
	return driveAPI
}
func (g *GoogleDrive) Destination() string { return "google-drive:" + g.FolderID }
func (g *GoogleDrive) request(ctx context.Context, method, path string, body io.Reader, contentType string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, path, body)
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := g.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Do not echo response bodies: authorization diagnostics can contain secrets.
		switch resp.StatusCode {
		case 401:
			return errors.New("Google authorization expired. Reconnect to Google Drive")
		case 403:
			return errors.New("Google Drive access denied or quota exceeded")
		case 404:
			return errors.New("Google Drive folder or file is unavailable")
		}
		return fmt.Errorf("Google Drive request failed (HTTP %d)", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}
func escapeQuery(value string) string {
	return strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(value)
}
func (g *GoogleDrive) list(ctx context.Context, query string) ([]driveFile, error) {
	var files []driveFile
	token := ""
	seen := map[string]bool{}
	for {
		params := url.Values{"q": {query}, "fields": {"nextPageToken,files(id,name,mimeType,size,parents,trashed,appProperties,md5Checksum)"}, "pageSize": {"1000"}, "spaces": {"drive"}}
		if token != "" {
			params.Set("pageToken", token)
		}
		var page struct {
			Files []driveFile `json:"files"`
			Next  string      `json:"nextPageToken"`
		}
		if err := g.request(ctx, "GET", g.base()+"/files?"+params.Encode(), nil, "", &page); err != nil {
			return nil, err
		}
		files = append(files, page.Files...)
		if page.Next == "" {
			return files, nil
		}
		if seen[page.Next] || page.Next == token {
			return nil, errors.New("invalid Drive pagination")
		}
		seen[page.Next] = true
		token = page.Next
	}
}
func (g *GoogleDrive) createFolder(ctx context.Context, name, parent string) (string, error) {
	data, _ := json.Marshal(driveFile{Name: name, MIME: folderMIME, Parents: []string{parent}})
	var file driveFile
	err := g.request(ctx, "POST", g.base()+"/files?fields=id", bytes.NewReader(data), "application/json", &file)
	if err == nil && file.ID == "" {
		err = errors.New("Drive returned an empty folder ID")
	}
	return file.ID, err
}

// SelectFolder verifies the Picker result before persisting it.
func (g *GoogleDrive) SelectFolder(ctx context.Context, id string) error {
	var folder driveFile
	if err := g.request(ctx, "GET", g.base()+"/files/"+url.PathEscape(id)+"?fields=id,name,mimeType,trashed,parents,driveId", nil, "", &folder); err != nil {
		return err
	}
	if folder.ID == "" || folder.ID != id || folder.DriveID != "" || folder.Name != g.folderName() || folder.MIME != folderMIME || folder.Trashed {
		return fmt.Errorf("select a folder named %s", g.folderName())
	}
	g.FolderID = folder.ID
	g.ExplicitFolder = true
	return nil
}
func (g *GoogleDrive) Prepare(ctx context.Context) error {
	name, err := NormalizeDriveFolderName(g.FolderName)
	if err != nil {
		return err
	}
	g.FolderName = name
	if len(g.folders) > 0 && time.Since(g.preparedAt) < 5*time.Minute {
		return ctx.Err()
	}
	// FolderIDs previously selected with Picker need not live in the root.
	roots, err := g.list(ctx, "trashed = false and mimeType = '"+folderMIME+"' and name = '"+escapeQuery(name)+"' and 'root' in parents")
	if err != nil {
		return err
	}
	if g.FolderID != "" {
		explicit := g.ExplicitFolder
		if err = g.SelectFolder(ctx, g.FolderID); err != nil {
			return err
		}
		g.ExplicitFolder = explicit
		found := false
		for _, root := range roots {
			if root.ID == g.FolderID {
				found = true
			}
		}
		if !found {
			roots = append(roots, driveFile{ID: g.FolderID})
		}
	}
	if len(roots) == 0 {
		id, err := g.createFolder(ctx, name, "root")
		if err != nil {
			return err
		}
		roots = append(roots, driveFile{ID: id})
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i].ID < roots[j].ID })
	if (!g.ExplicitFolder || g.FolderID == "") && g.FolderID != roots[0].ID {
		g.FolderID = roots[0].ID
		if g.OnFolder != nil {
			g.OnFolder(g.FolderID)
		}
	}
	g.folders = nil
	for _, root := range roots {
		children, err := g.list(ctx, "trashed = false and mimeType = '"+folderMIME+"' and name = 'signatures' and '"+escapeQuery(root.ID)+"' in parents")
		if err != nil {
			return err
		}
		if len(children) == 0 {
			id, err := g.createFolder(ctx, "signatures", root.ID)
			if err != nil {
				return err
			}
			children = []driveFile{{ID: id}}
		}
		sort.Slice(children, func(i, j int) bool { return children[i].ID < children[j].ID })
		// Put the selected destination first; other duplicate folders are read-only.
		var ids []string
		for _, folder := range children {
			ids = append(ids, folder.ID)
		}
		if root.ID == g.FolderID {
			g.folders = append(ids, g.folders...)
		} else {
			g.folders = append(g.folders, ids...)
		}
	}
	if g.uploaded == nil {
		g.uploaded = map[string][32]byte{}
	}
	g.preparedAt = time.Now()
	return nil
}
func (g *GoogleDrive) Download(ctx context.Context) ([]signatures.Operation, error) {
	var ops []signatures.Operation
	var failures []error
	for _, folder := range g.folders {
		files, err := g.list(ctx, "trashed = false and '"+escapeQuery(folder)+"' in parents")
		if err != nil {
			failures = append(failures, err)
			continue
		}
		for _, file := range files {
			if !strings.HasSuffix(file.Name, ".json") || file.MIME == folderMIME {
				continue
			}
			size, err := strconv.ParseInt(file.Size, 10, 64)
			if err != nil || size > signatures.MaxOperationSize || size < 0 {
				failures = append(failures, errors.New("remote signature has invalid size"))
				continue
			}
			op, err := g.download(ctx, file.ID)
			if err == nil && file.Name != op.ID+".json" {
				err = errors.New("remote operation filename mismatch")
			}
			if err != nil {
				failures = append(failures, err)
				continue
			}
			if previous, ok := g.uploaded[op.ID]; ok && previous != sha256.Sum256(mustJSON(op)) {
				failures = append(failures, errors.New("conflicting remote operation ID"))
				continue
			}
			ops = append(ops, op)
			g.uploaded[op.ID] = sha256.Sum256(mustJSON(op))
		}
	}
	return ops, errors.Join(failures...)
}
func (g *GoogleDrive) download(ctx context.Context, id string) (signatures.Operation, error) {
	var op signatures.Operation
	req, err := http.NewRequestWithContext(ctx, "GET", g.base()+"/files/"+url.PathEscape(id)+"?alt=media", nil)
	if err != nil {
		return op, err
	}
	resp, err := g.Client.Do(req)
	if err != nil {
		return op, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return op, fmt.Errorf("download signature failed (HTTP %d)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, signatures.MaxOperationSize+1))
	if err != nil {
		return op, err
	}
	return signatures.DecodeOperation(data)
}
func (g *GoogleDrive) Upload(ctx context.Context, op signatures.Operation) error {
	if err := op.Validate(); err != nil {
		return err
	}
	if len(g.folders) == 0 {
		return errors.New("Drive folder is not ready")
	}
	if previous, ok := g.uploaded[op.ID]; ok {
		if previous != sha256.Sum256(mustJSON(op)) {
			return errors.New("conflicting remote operation ID")
		}
		return nil
	}
	// A previous upload can have succeeded despite a lost response. Check its
	// logical operation ID before retrying; duplicate physical files are harmless.
	files, err := g.list(ctx, "trashed = false and name = '"+op.ID+".json' and '"+escapeQuery(g.folders[0])+"' in parents")
	if err != nil {
		return err
	}
	for _, file := range files {
		previous, err := g.download(ctx, file.ID)
		if err != nil {
			return err
		}
		a, _ := json.Marshal(previous)
		b, _ := json.Marshal(op)
		if !bytes.Equal(a, b) {
			return errors.New("conflicting remote operation ID")
		}
		g.uploaded[op.ID] = sha256.Sum256(mustJSON(op))
		return nil
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadata, _ := json.Marshal(driveFile{Name: op.ID + ".json", MIME: "application/json", Parents: []string{g.folders[0]}, Properties: map[string]string{"proviewFormat": "1"}})
	for _, data := range [][]byte{metadata, mustJSON(op)} {
		part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": []string{"application/json"}})
		if err != nil {
			return err
		}
		if _, err = part.Write(data); err != nil {
			return err
		}
	}
	if err = writer.Close(); err != nil {
		return err
	}
	uploadBase := strings.Replace(g.base(), "/drive/v3", "/upload/drive/v3", 1)
	err = g.request(ctx, "POST", uploadBase+"/files?uploadType=multipart", &body, "multipart/related; boundary="+writer.Boundary(), nil)
	if err == nil {
		g.uploaded[op.ID] = sha256.Sum256(mustJSON(op))
	}
	return err
}
func mustJSON(v any) []byte { data, _ := json.Marshal(v); return data }
