package cloud

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"emanuelelongo/proview/pkg/signatures"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

const s3IndexName = "current-index.json"

var errS3Changed = errors.New("S3 state changed during synchronization")

type cachedS3Index struct {
	index currentStateIndex
	etag  string
}

// SyncCurrent publishes immutable payloads before a conditional index update.
// Unreferenced objects remain recoverable without requiring DeleteObject access.
func (p *S3) SyncCurrent(ctx context.Context, store *signatures.Store) error {
	for attempt := 0; attempt < 4; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err := p.syncCurrent(ctx, store)
		if !errors.Is(err, errS3Changed) {
			return err
		}
		p.stateCache = nil
	}
	return errors.New("S3 changed repeatedly. Synchronize again")
}

func (p *S3) syncCurrent(ctx context.Context, store *signatures.Store) error {
	journal, err := store.Operations()
	if err != nil {
		return err
	}
	known := map[string]signatures.Operation{}
	for _, op := range journal {
		known[op.ID] = op
	}
	index, etag, err := p.readStateIndex(ctx)
	if err != nil {
		return err
	}
	var remote []signatures.Operation
	if etag == "" {
		// The legacy archive is read only during migration, never on routine syncs.
		remote, err = p.readLegacyState(ctx, known)
	} else {
		remote = make([]signatures.Operation, len(index.Records))
		err = parallelTransfers(len(remote), func(i int) error {
			record := index.Records[i]
			op := record.Operation
			if op.Deleted {
				remote[i] = op
				return nil
			}
			if cached, ok := known[op.ID]; ok && operationHash(cached) == record.Hash {
				op = cached
			} else {
				data, _, err := p.readStateObject(ctx, record.FileID, "", signatures.MaxOperationSize)
				if err != nil {
					return s3Error(ctx, err)
				}
				op, err = signatures.DecodeOperation(data)
				if err != nil {
					return errS3Operation
				}
			}
			header := op
			header.Entry = nil
			if operationHash(op) != record.Hash || !bytes.Equal(mustJSON(header), mustJSON(record.Operation)) {
				return errS3Operation
			}
			remote[i] = op
			return nil
		})
	}
	if err != nil {
		return err
	}
	state, err := signatures.CompactSyncState(remote, index.Aliases)
	if err != nil {
		return err
	}
	if err = store.MergeSyncState(state); err != nil {
		return err
	}
	local, err := store.CurrentSyncState()
	if err != nil {
		return err
	}
	if len(local.Operations) > 10000 {
		return errors.New("Too many signatures in the cloud index")
	}
	previous := map[string]currentStateRecord{}
	for _, record := range index.Records {
		previous[record.Operation.ID] = record
	}
	next := currentStateIndex{Version: 2, Aliases: local.Aliases, Records: make([]currentStateRecord, len(local.Operations))}
	err = parallelTransfers(len(local.Operations), func(i int) error {
		op := local.Operations[i]
		header := op
		header.Entry = nil
		record := currentStateRecord{Operation: header}
		if !op.Deleted {
			record.Hash = operationHash(op)
			record.FileID = p.payloadKey(op.ID)
			old, exists := previous[op.ID]
			if !exists || old.Hash != record.Hash {
				if err := p.ensureStatePayload(ctx, op); err != nil {
					return err
				}
			}
		}
		next.Records[i] = record
		return nil
	})
	if err != nil {
		return err
	}
	if etag == "" || !bytes.Equal(mustJSON(index), mustJSON(next)) {
		data := mustJSON(next)
		if len(data) > maxCurrentIndexSize {
			return errors.New("Signature index is too large")
		}
		input := &s3.PutObjectInput{Bucket: aws.String(p.Config.Bucket), Key: aws.String(p.prefix() + s3IndexName), Body: bytes.NewReader(data), ContentType: aws.String("application/json")}
		if etag == "" {
			input.IfNoneMatch = aws.String("*")
		} else {
			input.IfMatch = aws.String(etag)
		}
		result, err := p.client.PutObject(ctx, input)
		if s3Status(err) == 409 || s3Status(err) == 412 {
			return errS3Changed
		}
		if err != nil {
			return s3Error(ctx, err)
		}
		etag = aws.ToString(result.ETag)
		// A missing response ETag disables caching; the next sync still reads
		// the committed index before attempting another conditional write.
		p.stateCache = nil
		if etag != "" {
			p.stateCache = &cachedS3Index{index: next, etag: etag}
		}
	}
	return acknowledgeCurrent(ctx, store, p.Destination(), local)
}

func (p *S3) payloadKey(id string) string { return p.prefix() + "current/" + id + ".json" }

func (p *S3) readStateObject(ctx context.Context, key, unchanged string, limit int) ([]byte, string, error) {
	input := &s3.GetObjectInput{Bucket: aws.String(p.Config.Bucket), Key: aws.String(key)}
	if unchanged != "" {
		input.IfNoneMatch = aws.String(unchanged)
	}
	result, err := p.client.GetObject(ctx, input)
	if err != nil {
		return nil, "", err
	}
	defer result.Body.Close()
	if result.ContentLength != nil && (*result.ContentLength < 0 || *result.ContentLength > int64(limit)) {
		return nil, "", errS3Operation
	}
	data, err := io.ReadAll(io.LimitReader(result.Body, int64(limit)+1))
	if len(data) > limit {
		return nil, "", errS3Operation
	}
	return data, aws.ToString(result.ETag), err
}

func (p *S3) readStateIndex(ctx context.Context) (currentStateIndex, string, error) {
	etag := ""
	if p.stateCache != nil {
		etag = p.stateCache.etag
	}
	data, token, err := p.readStateObject(ctx, p.prefix()+s3IndexName, etag, maxCurrentIndexSize)
	if s3Status(err) == 304 && p.stateCache != nil {
		return p.stateCache.index, etag, nil
	}
	if s3Status(err) == 404 {
		p.stateCache = nil
		return currentStateIndex{Version: 2}, "", nil
	}
	if err != nil {
		p.preparedAt = time.Time{}
		return currentStateIndex{}, "", s3Error(ctx, err)
	}
	if token == "" {
		return currentStateIndex{}, "", errors.New("S3 did not provide a conditional update ETag")
	}
	var index currentStateIndex
	if json.Unmarshal(data, &index) != nil {
		return index, "", errors.New("Invalid signature index")
	}
	if err = validateCurrentIndex(index); err != nil {
		return index, "", err
	}
	for _, record := range index.Records {
		if !record.Operation.Deleted && record.FileID != p.payloadKey(record.Operation.ID) {
			return index, "", errS3Operation
		}
	}
	if len(index.Retired) != 0 {
		return index, "", errors.New("Unsupported S3 cleanup records")
	}
	p.stateCache = &cachedS3Index{index: index, etag: token}
	return index, token, nil
}

func (p *S3) ensureStatePayload(ctx context.Context, op signatures.Operation) error {
	data := mustJSON(op)
	if len(data) > signatures.MaxOperationSize {
		return errS3Operation
	}
	_, err := p.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(p.Config.Bucket), Key: aws.String(p.payloadKey(op.ID)), Body: bytes.NewReader(data), ContentType: aws.String("application/json"), IfNoneMatch: aws.String("*")})
	if s3Status(err) == 409 {
		return errS3Changed
	}
	if s3Status(err) == 412 {
		existing, _, readErr := p.readStateObject(ctx, p.payloadKey(op.ID), "", signatures.MaxOperationSize)
		if readErr != nil {
			return s3Error(ctx, readErr)
		}
		decoded, decodeErr := signatures.DecodeOperation(existing)
		if decodeErr != nil || operationHash(decoded) != operationHash(op) {
			return errors.New("Conflicting current signature payload")
		}
		return nil
	}
	return s3Error(ctx, err)
}

func (p *S3) readLegacyState(ctx context.Context, known map[string]signatures.Operation) ([]signatures.Operation, error) {
	type legacyObject struct{ key, etag string }
	var objects []legacyObject
	paginator := s3.NewListObjectsV2Paginator(p.client, &s3.ListObjectsV2Input{Bucket: aws.String(p.Config.Bucket), Prefix: aws.String(p.prefix())})
	seen := map[string]bool{}
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, s3Error(ctx, err)
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			name := strings.TrimPrefix(key, p.prefix())
			if !strings.HasPrefix(key, p.prefix()) || strings.Contains(name, "/") || !strings.HasSuffix(name, ".json") || name == s3IndexName {
				continue
			}
			if object.Size == nil || *object.Size < 0 || *object.Size > signatures.MaxOperationSize || !validStateHex(strings.TrimSuffix(name, ".json"), 16) {
				return nil, errS3Operation
			}
			objects = append(objects, legacyObject{key, aws.ToString(object.ETag)})
		}
		token := aws.ToString(page.NextContinuationToken)
		if aws.ToBool(page.IsTruncated) {
			if token == "" || seen[token] {
				return nil, errors.New("Invalid S3 pagination")
			}
			seen[token] = true
		}
	}
	ops := make([]signatures.Operation, len(objects))
	err := parallelTransfers(len(objects), func(i int) error {
		object := objects[i]
		id := strings.TrimSuffix(strings.TrimPrefix(object.key, p.prefix()), ".json")
		if op, ok := known[id]; ok {
			checksum := md5.Sum(mustJSON(op))
			if strings.Trim(object.etag, "\"") == hex.EncodeToString(checksum[:]) {
				ops[i] = op
				return nil
			}
		}
		op, err := p.read(ctx, object.key)
		if err != nil {
			return s3Error(ctx, err)
		}
		ops[i] = op
		return nil
	})
	return ops, err
}
