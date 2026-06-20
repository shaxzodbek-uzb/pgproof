package destination

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

// s3Dest stores backups in any S3-compatible object store (AWS S3, Cloudflare
// R2, DigitalOcean Spaces, MinIO, ...).
type s3Dest struct {
	name   string
	client *minio.Client
	bucket string
}

func newS3(cfg config.Destination) (Destination, error) {
	host, secure := parseS3Endpoint(cfg.Endpoint, cfg.Region)
	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
		Region: cfg.Region,
	}
	if cfg.PathStyle {
		opts.BucketLookup = minio.BucketLookupPath
	}
	client, err := minio.New(host, opts)
	if err != nil {
		return nil, fmt.Errorf("init s3 client: %w", err)
	}
	return &s3Dest{name: cfg.Name, client: client, bucket: cfg.Bucket}, nil
}

func parseS3Endpoint(raw, region string) (host string, secure bool) {
	if raw == "" {
		if region != "" && region != "us-east-1" {
			return "s3." + region + ".amazonaws.com", true
		}
		return "s3.amazonaws.com", true
	}
	secure = true
	switch {
	case strings.HasPrefix(raw, "http://"):
		secure, raw = false, raw[len("http://"):]
	case strings.HasPrefix(raw, "https://"):
		raw = raw[len("https://"):]
	}
	return strings.TrimSuffix(raw, "/"), secure
}

func (d *s3Dest) Name() string   { return d.name }
func (d *s3Dest) Type() string   { return config.TypeS3 }
func (d *s3Dest) Readable() bool { return true }

func (d *s3Dest) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := d.client.PutObject(ctx, d.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("s3 put %q: %w", key, err)
	}
	return nil
}

func (d *s3Dest) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := d.client.GetObject(ctx, d.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("s3 get %q: %w", key, err)
	}
	// Surface a missing object now rather than on first Read.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, fmt.Errorf("s3 get %q: %w", key, err)
	}
	return obj, nil
}

func (d *s3Dest) List(ctx context.Context, prefix string) ([]Artifact, error) {
	var out []Artifact
	for o := range d.client.ListObjects(ctx, d.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if o.Err != nil {
			return nil, fmt.Errorf("s3 list %q: %w", prefix, o.Err)
		}
		out = append(out, Artifact{Key: o.Key, Size: o.Size, ModTime: o.LastModified})
	}
	return out, nil
}

func (d *s3Dest) Delete(ctx context.Context, key string) error {
	return d.client.RemoveObject(ctx, d.bucket, key, minio.RemoveObjectOptions{})
}

// Test verifies the bucket is reachable and exists.
func (d *s3Dest) Test(ctx context.Context) error {
	ok, err := d.client.BucketExists(ctx, d.bucket)
	if err != nil {
		return fmt.Errorf("reach bucket %q: %w", d.bucket, err)
	}
	if !ok {
		return fmt.Errorf("bucket %q does not exist or is not accessible with these credentials", d.bucket)
	}
	return nil
}
