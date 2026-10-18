package cloud

import (
	"bytes"
	"context"
	"crypto/sha256"
	"emanuelelongo/proview/internal/appprofile"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"emanuelelongo/proview/pkg/signatures"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
	"github.com/zalando/go-keyring"
)

// S3Config contains destination settings only; credentials belong in the keyring.
type S3Config struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	Prefix    string `json:"prefix"`
	PathStyle bool   `json:"pathStyle"`
}

type S3Keys struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

type S3Credentials interface {
	Get() (S3Keys, error)
	Set(S3Keys) error
	Delete() error
}
type SystemS3Credentials struct{}

var errS3Operation = errors.New("Invalid or unreadable remote signature operation")

func (SystemS3Credentials) Get() (S3Keys, error) {
	data, err := keyring.Get(appprofile.CredentialService("s3"), "credentials")
	if err != nil {
		return S3Keys{}, err
	}
	var keys S3Keys
	err = json.Unmarshal([]byte(data), &keys)
	return keys, err
}
func (SystemS3Credentials) Set(keys S3Keys) error {
	data, err := json.Marshal(keys)
	if err != nil {
		return err
	}
	return keyring.Set(appprofile.CredentialService("s3"), "credentials", string(data))
}
func (SystemS3Credentials) Delete() error {
	err := keyring.Delete(appprofile.CredentialService("s3"), "credentials")
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

func (c S3Config) Normalize() (S3Config, error) {
	c.Endpoint = strings.TrimRight(strings.TrimSpace(c.Endpoint), "/")
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return c, errors.New("Enter an S3 endpoint URL without credentials, query or fragment")
	}
	u.Host = strings.ToLower(u.Host)
	c.Endpoint = u.String()
	c.Region = strings.TrimSpace(c.Region)
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	c.Bucket = strings.TrimSpace(c.Bucket)
	if c.Bucket == "" || strings.ContainsAny(c.Bucket, "/\\ \t\n\r") {
		return c, errors.New("Enter a bucket name")
	}
	c.Prefix = strings.Trim(strings.TrimSpace(c.Prefix), "/")
	if c.Prefix == "" {
		c.Prefix = "proview/signatures"
	}
	if len(c.Prefix) > 900 || strings.Contains(c.Prefix, "\\") {
		return c, errors.New("Invalid S3 prefix")
	}
	for _, part := range strings.Split(c.Prefix, "/") {
		if part == "" || part == "." || part == ".." {
			return c, errors.New("Invalid S3 prefix")
		}
	}
	return c, nil
}

type S3 struct {
	Config     S3Config
	client     *s3.Client
	preparedAt time.Time
	stateCache *cachedS3Index
}

func NewS3(config S3Config, keys S3Keys) (*S3, error) {
	config, err := config.Normalize()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(keys.AccessKey) == "" || keys.SecretKey == "" {
		return nil, errors.New("Enter the S3 access key and secret key")
	}
	client := s3.New(s3.Options{
		Region: config.Region, BaseEndpoint: aws.String(config.Endpoint), UsePathStyle: config.PathStyle,
		Credentials:                credentials.NewStaticCredentialsProvider(keys.AccessKey, keys.SecretKey, ""),
		HTTPClient:                 &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	return &S3{Config: config, client: client}, nil
}

func (p *S3) Destination() string {
	// Credentials, region and addressing mode do not change the remote archive.
	data, _ := json.Marshal([]string{p.Config.Endpoint, p.Config.Bucket, p.Config.Prefix})
	return fmt.Sprintf("s3:%x", sha256.Sum256(data))
}
func (p *S3) prefix() string { return p.Config.Prefix + "/" }
func (p *S3) Prepare(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !p.preparedAt.IsZero() && time.Since(p.preparedAt) < 5*time.Minute {
		return nil
	}
	// Scope the check to the archive: no bucket creation or account-wide listing.
	_, err := p.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(p.Config.Bucket), Prefix: aws.String(p.prefix()), MaxKeys: aws.Int32(1)})
	if err == nil {
		p.preparedAt = time.Now()
	}
	return s3Error(ctx, err)
}
func s3Status(err error) int {
	var response *smithyhttp.ResponseError
	if errors.As(err, &response) {
		return response.HTTPStatusCode()
	}
	return 0
}
func s3Error(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, errS3Operation) {
		return errS3Operation
	}
	// SDK errors can include endpoint URLs, response bodies and access key IDs.
	switch status := s3Status(err); status {
	case 401, 403:
		return errors.New("S3 access denied. Check credentials, permissions and region")
	case 404:
		return errors.New("S3 bucket or object is unavailable")
	case 409, 412:
		return errors.New("S3 object changed during upload. Retry synchronization")
	case 400, 501:
		return errors.New("S3 request rejected. Check endpoint, region and support for conditional uploads")
	case 0:
		return errors.New("Cannot reach the S3 endpoint")
	default:
		return fmt.Errorf("S3 request failed (HTTP %d)", status)
	}
}
func (p *S3) read(ctx context.Context, key string) (signatures.Operation, error) {
	var op signatures.Operation
	result, err := p.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(p.Config.Bucket), Key: aws.String(key)})
	if err != nil {
		return op, err
	}
	defer result.Body.Close()
	if result.ContentLength != nil && (*result.ContentLength < 0 || *result.ContentLength > signatures.MaxOperationSize) {
		return op, errS3Operation
	}
	data, err := io.ReadAll(io.LimitReader(result.Body, signatures.MaxOperationSize+1))
	if err != nil {
		return op, err
	}
	op, err = signatures.DecodeOperation(data)
	if err != nil || key != p.prefix()+op.ID+".json" {
		err = errS3Operation
	}
	return op, err
}
func (p *S3) Download(ctx context.Context) ([]signatures.Operation, error) {
	var ops []signatures.Operation
	var failures []error
	paginator := s3.NewListObjectsV2Paginator(p.client, &s3.ListObjectsV2Input{Bucket: aws.String(p.Config.Bucket), Prefix: aws.String(p.prefix())})
	seen := map[string]bool{}
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return ops, errors.Join(append(failures, s3Error(ctx, err))...)
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			name := strings.TrimPrefix(key, p.prefix())
			if !strings.HasPrefix(key, p.prefix()) || strings.Contains(name, "/") || !strings.HasSuffix(name, ".json") {
				continue
			}
			if object.Size == nil || *object.Size < 0 || *object.Size > signatures.MaxOperationSize {
				failures = append(failures, errors.New("Remote signature has invalid size"))
				continue
			}
			op, err := p.read(ctx, key)
			if err != nil {
				failures = append(failures, s3Error(ctx, err))
				continue
			}
			ops = append(ops, op)
		}
		token := aws.ToString(page.NextContinuationToken)
		if aws.ToBool(page.IsTruncated) {
			if token == "" || seen[token] {
				return ops, errors.Join(append(failures, errors.New("Invalid S3 pagination"))...)
			}
			seen[token] = true
		}
	}
	return ops, errors.Join(failures...)
}
func (p *S3) Upload(ctx context.Context, op signatures.Operation) error {
	if err := op.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	if len(data) > signatures.MaxOperationSize {
		return errors.New("Signature operation is too large")
	}
	key := p.prefix() + op.ID + ".json"
	previous, err := p.read(ctx, key)
	if err == nil {
		return sameS3Operation(previous, op)
	}
	if s3Status(err) != 404 {
		return s3Error(ctx, err)
	}
	_, err = p.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(p.Config.Bucket), Key: aws.String(key), Body: bytes.NewReader(data),
		ContentType: aws.String("application/json"), IfNoneMatch: aws.String("*"),
	})
	if s3Status(err) == 412 {
		previous, err = p.read(ctx, key)
		if err == nil {
			return sameS3Operation(previous, op)
		}
	}
	return s3Error(ctx, err)
}
func sameS3Operation(a, b signatures.Operation) error {
	if !bytes.Equal(mustJSON(a), mustJSON(b)) {
		return errors.New("Conflicting remote operation ID")
	}
	return nil
}
