package cloud

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"emanuelelongo/proview/pkg/signatures"
)

const driveIndexName = "current-index.json"

var errDriveChanged = errors.New("Google Drive state changed during synchronization")

type cachedDriveIndex struct {
	index          currentStateIndex
	checksum, etag string
}
type driveIndexFile struct {
	file driveFile
	cachedDriveIndex
	folder string
}
type driveListedFile struct {
	file   driveFile
	folder string
}

// SyncCurrent reads a lightweight index, transfers changed payloads, and merges
// a conditional index write. Retries never replace a concurrently newer index.
func (g *GoogleDrive) SyncCurrent(ctx context.Context, store *signatures.Store) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(g.folders) == 0 {
		return errors.New("Drive folder is not ready")
	}
	for attempt := 0; attempt < 4; attempt++ {
		err := g.syncCurrent(ctx, store)
		if !errors.Is(err, errDriveChanged) {
			return err
		}
		g.stateCache = nil
	}
	return errors.New("Google Drive changed repeatedly. Synchronize again")
}
func (g *GoogleDrive) syncCurrent(ctx context.Context, store *signatures.Store) error {
	var local signatures.SyncState
	known := map[string]signatures.Operation{}
	journal, err := store.Operations()
	if err != nil {
		return err
	}
	for _, op := range journal {
		known[op.ID] = op
	}
	var files []driveListedFile
	for _, folder := range g.folders {
		listed, err := g.list(ctx, "trashed = false and '"+escapeQuery(folder)+"' in parents")
		if err != nil {
			g.preparedAt = time.Time{}
			return err
		}
		for _, file := range listed {
			files = append(files, driveListedFile{file, folder})
		}
	}
	var indexes []driveIndexFile
	var legacy []driveListedFile
	for _, listed := range files {
		f := listed.file
		if f.MIME == folderMIME {
			continue
		}
		if f.Name == driveIndexName {
			indexes = append(indexes, driveIndexFile{file: f, folder: listed.folder})
			continue
		}
		if strings.HasSuffix(f.Name, ".json") && !strings.HasPrefix(f.Name, "current-") {
			legacy = append(legacy, listed)
		}
	}
	err = parallelTransfers(len(indexes), func(i int) error {
		cache, err := g.readCurrentIndex(ctx, indexes[i].file)
		if err == nil {
			indexes[i].cachedDriveIndex = cache
		}
		return err
	})
	if err != nil {
		return err
	}
	aliases := map[string]string{}
	var records []currentStateRecord
	for _, index := range indexes {
		aliases = mergeStateAliases(aliases, index.index.Aliases)
		records = append(records, index.index.Records...)
	}
	remote := make([]signatures.Operation, len(records)+len(legacy))
	err = parallelTransfers(len(remote), func(i int) error {
		if i >= len(records) {
			f := legacy[i-len(records)].file
			if err := validateDriveSize(f, signatures.MaxOperationSize); err != nil {
				return err
			}
			id := strings.TrimSuffix(f.Name, ".json")
			if cached, ok := known[id]; ok && f.Checksum != "" {
				checksum := md5.Sum(mustJSON(cached))
				if hex.EncodeToString(checksum[:]) == f.Checksum {
					remote[i] = cached
					return nil
				}
			}
			op, err := g.download(ctx, f.ID)
			if err != nil {
				return err
			}
			if f.Name != op.ID+".json" {
				return errors.New("remote operation filename mismatch")
			}
			remote[i] = op
			return nil
		}
		record := records[i]
		op := record.Operation
		if op.Deleted {
			if err := op.Validate(); err != nil {
				return err
			}
			remote[i] = op
			return nil
		}
		if cached, ok := known[op.ID]; ok && operationHash(cached) == record.Hash {
			header := cached
			header.Entry = nil
			if !bytes.Equal(mustJSON(header), mustJSON(op)) {
				return errors.New("signature index header mismatch")
			}
			remote[i] = cached
			return nil
		}
		downloaded, err := g.download(ctx, record.FileID)
		if err != nil {
			return errDriveChanged
		}
		header := downloaded
		header.Entry = nil
		if !bytes.Equal(mustJSON(header), mustJSON(op)) || operationHash(downloaded) != record.Hash {
			return errDriveChanged
		}
		remote[i] = downloaded
		return nil
	})
	if err != nil {
		return err
	}
	remoteState, err := signatures.CompactSyncState(remote, aliases)
	if err != nil {
		return err
	}
	if err = store.MergeSyncState(remoteState); err != nil {
		return err
	}
	// Include edits made locally while downloads were in flight.
	local, err = store.CurrentSyncState()
	if err != nil {
		return err
	}
	recordByID := map[string]currentStateRecord{}
	for _, record := range records {
		recordByID[record.Operation.ID] = record
	}
	if len(local.Operations) > 10000 {
		return errors.New("too many signatures in the cloud index")
	}
	next := currentStateIndex{Version: 2, Aliases: local.Aliases}
	next.Records = make([]currentStateRecord, len(local.Operations))
	err = parallelTransfers(len(local.Operations), func(i int) error {
		op := local.Operations[i]
		header := op
		header.Entry = nil
		record := currentStateRecord{Operation: header}
		if !op.Deleted {
			record.Hash = operationHash(op)
			if existing, ok := recordByID[op.ID]; ok && existing.Hash == record.Hash {
				record.FileID = existing.FileID
			} else {
				id, err := g.ensureCurrentPayload(ctx, op, files)
				if err != nil {
					return err
				}
				record.FileID = id
			}
		}
		next.Records[i] = record
		return nil
	})
	if err != nil {
		return err
	}
	var target *driveIndexFile
	for i := range indexes {
		index := &indexes[i]
		if index.folder == g.folders[0] && (target == nil || index.file.ID < target.file.ID) {
			target = index
		}
	}
	// Only retire files observed before publication. Newly uploaded concurrent
	// payloads are never treated as garbage. Trash keeps migration recoverable.
	retained := map[string]bool{}
	for _, record := range next.Records {
		retained[record.FileID] = true
	}
	for _, index := range indexes {
		if target == nil || index.file.ID != target.file.ID {
			for _, record := range index.index.Records {
				retained[record.FileID] = true
			}
		}
	}
	var obsolete []string
	for _, file := range legacy {
		if !retained[file.file.ID] {
			obsolete = append(obsolete, file.file.ID)
		}
	}
	if target != nil {
		for _, record := range target.index.Records {
			if record.FileID != "" && !retained[record.FileID] {
				obsolete = append(obsolete, record.FileID)
			}
		}
	}
	if target != nil {
		visible := map[string]bool{}
		for _, listed := range files {
			if isDrivePayloadName(listed.file.Name) && listed.file.MIME != folderMIME {
				visible[listed.file.ID] = true
			}
		}
		for _, id := range target.index.Retired {
			if visible[id] && !retained[id] {
				obsolete = append(obsolete, id)
			}
		}
	}
	sort.Strings(obsolete)
	obsolete = slices.Compact(obsolete)
	next.Retired = obsolete
	if target == nil {
		file, err := g.createCurrentFile(ctx, driveIndexName, mustJSON(next))
		if err != nil {
			return err
		}
		if file.ID == "" {
			return errors.New("Drive returned an empty index ID")
		}
		g.cachePublishedIndex(file.ID, next)
	} else if !bytes.Equal(mustJSON(target.index), mustJSON(next)) {
		if err = g.updateCurrentIndex(ctx, target.file.ID, target.etag, target.checksum, next); err != nil {
			return err
		}
		g.cachePublishedIndex(target.file.ID, next)
	}
	// Acknowledge only operations represented by the committed current state.
	if err := acknowledgeCurrent(ctx, store, g.Destination(), local); err != nil {
		return err
	}
	err = parallelTransfers(len(obsolete), func(i int) error {
		return g.request(ctx, "PATCH", g.base()+"/files/"+url.PathEscape(obsolete[i]), bytes.NewReader([]byte(`{"trashed":true}`)), "application/json", nil)
	})
	if err != nil {
		return fmt.Errorf("current signatures synchronized, but old cloud files could not be retired: %w", err)
	}
	// The next metadata listing detects changed indexes, including other writers.
	return nil
}
func validateDriveSize(file driveFile, limit int) error {
	var size int64
	if _, err := fmt.Sscan(file.Size, &size); err != nil || size < 0 || size > int64(limit) {
		return errors.New("remote signature has invalid size")
	}
	return nil
}
func (g *GoogleDrive) v2Base() string { return strings.Replace(g.base(), "/drive/v3", "/drive/v2", 1) }
func (g *GoogleDrive) readCurrentIndex(ctx context.Context, file driveFile) (cachedDriveIndex, error) {
	if err := validateDriveSize(file, maxCurrentIndexSize); err != nil {
		return cachedDriveIndex{}, err
	}
	// Cache access is serialized separately from concurrent payload transfers.
	g.indexMu.Lock()
	cached, ok := g.stateCache[file.ID]
	g.indexMu.Unlock()
	if ok && file.Checksum != "" && cached.checksum == file.Checksum {
		return cached, nil
	}
	req, err := http.NewRequestWithContext(ctx, "GET", g.base()+"/files/"+url.PathEscape(file.ID)+"?alt=media", nil)
	if err != nil {
		return cachedDriveIndex{}, err
	}
	resp, err := g.Client.Do(req)
	if err != nil {
		return cachedDriveIndex{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 412 {
		return cachedDriveIndex{}, errDriveChanged
	}
	if resp.StatusCode != 200 {
		return cachedDriveIndex{}, fmt.Errorf("download signature index failed (HTTP %d)", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxCurrentIndexSize+1))
	if err != nil {
		return cachedDriveIndex{}, err
	}
	if len(data) > maxCurrentIndexSize {
		return cachedDriveIndex{}, errors.New("signature index is too large")
	}
	checksum := md5.Sum(data)
	actual := hex.EncodeToString(checksum[:])
	if file.Checksum != "" && actual != file.Checksum {
		return cachedDriveIndex{}, errDriveChanged
	}
	// Metadata and media have different ETags on Drive. Bind the downloaded
	// bytes to the update token through the content checksum, not a media If-Match.
	var meta struct {
		ETag     string `json:"etag"`
		Checksum string `json:"md5Checksum"`
	}
	if err := g.request(ctx, "GET", g.v2Base()+"/files/"+url.PathEscape(file.ID)+"?fields=etag,md5Checksum", nil, "", &meta); err != nil {
		return cachedDriveIndex{}, err
	}
	if meta.ETag == "" || meta.Checksum == "" {
		return cachedDriveIndex{}, errors.New("Google Drive did not provide conditional update metadata")
	}
	if meta.Checksum != actual {
		return cachedDriveIndex{}, errDriveChanged
	}
	var index currentStateIndex
	if err = json.Unmarshal(data, &index); err != nil {
		return cachedDriveIndex{}, errors.New("invalid signature index")
	}
	if err := validateCurrentIndex(index); err != nil {
		return cachedDriveIndex{}, err
	}
	cached = cachedDriveIndex{index: index, checksum: actual, etag: meta.ETag}
	g.indexMu.Lock()
	if g.stateCache == nil {
		g.stateCache = map[string]cachedDriveIndex{}
	}
	g.stateCache[file.ID] = cached
	g.indexMu.Unlock()
	return cached, nil
}
func (g *GoogleDrive) updateCurrentIndex(ctx context.Context, id, etag, expectedChecksum string, index currentStateIndex) error {
	data := mustJSON(index)
	if len(data) > maxCurrentIndexSize {
		return errors.New("signature index is too large")
	}
	if etag == "" {
		var meta struct {
			ETag     string `json:"etag"`
			Checksum string `json:"md5Checksum"`
		}
		if err := g.request(ctx, "GET", g.v2Base()+"/files/"+url.PathEscape(id)+"?fields=etag,md5Checksum", nil, "", &meta); err != nil {
			return err
		}
		if meta.ETag == "" || meta.Checksum == "" {
			return errors.New("Google Drive did not provide conditional update metadata")
		}
		if meta.Checksum != expectedChecksum {
			return errDriveChanged
		}
		etag = meta.ETag
	}
	path := strings.Replace(g.v2Base(), "/drive/v2", "/upload/drive/v2", 1) + "/files/" + url.PathEscape(id) + "?uploadType=media&newRevision=false"
	req, err := http.NewRequestWithContext(ctx, "PUT", path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", etag)
	resp, err := g.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 412 {
		return errDriveChanged
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("update signature index failed (HTTP %d)", resp.StatusCode)
	}
	return nil
}
func (g *GoogleDrive) createCurrentFile(ctx context.Context, name string, data []byte) (driveFile, error) {
	if (name == driveIndexName && len(data) > maxCurrentIndexSize) || len(data) > signatures.MaxOperationSize {
		return driveFile{}, errors.New("signature sync file is too large")
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadata := mustJSON(driveFile{Name: name, MIME: "application/json", Parents: []string{g.folders[0]}, Properties: map[string]string{"proviewFormat": "2"}})
	for _, partData := range [][]byte{metadata, data} {
		part, err := writer.CreatePart(textproto.MIMEHeader{"Content-Type": []string{"application/json"}})
		if err != nil {
			return driveFile{}, err
		}
		if _, err = part.Write(partData); err != nil {
			return driveFile{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return driveFile{}, err
	}
	var file driveFile
	err := g.request(ctx, "POST", strings.Replace(g.base(), "/drive/v3", "/upload/drive/v3", 1)+"/files?uploadType=multipart&fields=id", &body, "multipart/related; boundary="+writer.Boundary(), &file)
	if err == nil && file.ID == "" {
		err = errors.New("Drive returned an empty file ID")
	}
	return file, err
}
func (g *GoogleDrive) ensureCurrentPayload(ctx context.Context, op signatures.Operation, files []driveListedFile) (string, error) {
	name := "current-" + op.ID + ".json"
	for _, listed := range files {
		if listed.folder == g.folders[0] && listed.file.Name == name {
			existing, err := g.download(ctx, listed.file.ID)
			if err != nil {
				return "", err
			}
			if operationHash(existing) != operationHash(op) {
				return "", errors.New("conflicting current signature payload")
			}
			return listed.file.ID, nil
		}
	}
	file, err := g.createCurrentFile(ctx, name, mustJSON(op))
	return file.ID, err
}

func (g *GoogleDrive) cachePublishedIndex(id string, index currentStateIndex) {
	checksum := md5.Sum(mustJSON(index))
	g.indexMu.Lock()
	defer g.indexMu.Unlock()
	if g.stateCache == nil {
		g.stateCache = map[string]cachedDriveIndex{}
	}
	g.stateCache[id] = cachedDriveIndex{index: index, checksum: hex.EncodeToString(checksum[:])}
}

func isDrivePayloadName(name string) bool {
	name = strings.TrimPrefix(name, "current-")
	return strings.HasSuffix(name, ".json") && validStateHex(strings.TrimSuffix(name, ".json"), 16)
}
