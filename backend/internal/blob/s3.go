package blob

import (
	"context"
	"errors"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3 stores blobs in an S3-compatible bucket (MinIO, Ceph, AWS, R2).
//
// Useful when the backend must stay stateless: with several replicas behind a
// load balancer a local volume cannot be shared, a bucket can.
type S3 struct {
	cli    *minio.Client
	bucket string
	prefix string
}

type S3Config struct {
	Endpoint  string // "minio:9000", without scheme
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string
	UseSSL    bool
	Prefix    string // usually "blobs"
}

func NewS3(ctx context.Context, c S3Config) (*S3, error) {
	cli, err := minio.New(c.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(c.AccessKey, c.SecretKey, ""),
		Secure: c.UseSSL,
		Region: c.Region,
	})
	if err != nil {
		return nil, err
	}
	exists, err := cli.BucketExists(ctx, c.Bucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := cli.MakeBucket(ctx, c.Bucket, minio.MakeBucketOptions{Region: c.Region}); err != nil {
			return nil, err
		}
	}
	prefix := c.Prefix
	if prefix == "" {
		prefix = "blobs"
	}
	return &S3{cli: cli, bucket: c.Bucket, prefix: prefix}, nil
}

func (s *S3) key(sha string) string { return s.prefix + "/" + sha[:2] + "/" + sha }

func (s *S3) Put(ctx context.Context, sha string, r io.Reader, size int64) error {
	ok, err := s.Has(ctx, sha)
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	_, err = s.cli.PutObject(ctx, s.bucket, s.key(sha), r, size,
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	return err
}

func (s *S3) Get(ctx context.Context, sha string) (io.ReadCloser, error) {
	obj, err := s.cli.GetObject(ctx, s.bucket, s.key(sha), minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	// GetObject is lazy: the "does not exist" error arrives on the first Stat, not here.
	if _, err := obj.Stat(); err != nil {
		obj.Close()
		if isNoSuchKey(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return obj, nil
}

func (s *S3) Has(ctx context.Context, sha string) (bool, error) {
	_, err := s.cli.StatObject(ctx, s.bucket, s.key(sha), minio.StatObjectOptions{})
	if err != nil {
		if isNoSuchKey(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *S3) Delete(ctx context.Context, sha string) error {
	err := s.cli.RemoveObject(ctx, s.bucket, s.key(sha), minio.RemoveObjectOptions{})
	if isNoSuchKey(err) {
		return nil
	}
	return err
}

func isNoSuchKey(err error) bool {
	if err == nil {
		return false
	}
	var resp minio.ErrorResponse
	if errors.As(err, &resp) {
		return resp.Code == "NoSuchKey" || resp.StatusCode == 404
	}
	return false
}
