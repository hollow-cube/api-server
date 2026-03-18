package object

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3manager "github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/fx"
	"go.uber.org/zap"
)

var _ Client = (*S3Client)(nil)

type S3Client struct {
	log        *zap.SugaredLogger
	rawClient  *s3.Client
	downloader *s3manager.Downloader
	uploader   *s3manager.Uploader
	bucket     string
}

type S3ClientParams struct {
	fx.In

	Lifecycle  fx.Lifecycle
	Log        *zap.SugaredLogger
	S3Client   *s3.Client
	Downloader *s3manager.Downloader
	Uploader   *s3manager.Uploader
}

func NewS3ClientFactory(bucket string) any {
	return func(p S3ClientParams) (Client, error) {
		p.Lifecycle.Append(fx.Hook{OnStart: func(ctx context.Context) error {
			buckets, err := p.S3Client.ListBuckets(ctx, nil)
			if err != nil {
				return fmt.Errorf("failed to list buckets: %w", err)
			}

			var found bool
			for _, b := range buckets.Buckets {
				if *b.Name == bucket {
					found = true
					break
				}
			}
			if !found {
				_, err := p.S3Client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: &bucket})
				if err != nil {
					return fmt.Errorf("failed to create bucket: %w", err)
				}
			}

			return nil
		}})

		return &S3Client{
			log:        p.Log.With("object", "s3"),
			rawClient:  p.S3Client,
			downloader: p.Downloader,
			uploader:   p.Uploader,
			bucket:     bucket,
		}, nil
	}
}

func (c *S3Client) Upload(ctx context.Context, key string, data []byte) error {
	req := &s3.PutObjectInput{Bucket: &c.bucket, Key: &key, Body: bytes.NewReader(data)}
	_, err := c.uploader.Upload(ctx, req)
	return err
}

func (c *S3Client) Download(ctx context.Context, key string) ([]byte, error) {
	var b s3manager.WriteAtBuffer
	req := &s3.GetObjectInput{Bucket: &c.bucket, Key: &key}
	_, err := c.downloader.Download(ctx, &b, req)
	if err != nil {
		var noKey *types.NoSuchKey
		if errors.As(err, &noKey) {
			return nil, ErrNotFound
		}

		return nil, err
	}

	data := b.Bytes()
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("r2.key", key),
		attribute.String("r2.local_md5", fmt.Sprintf(`"%x"`, md5.Sum(data))),
	)

	return b.Bytes(), nil
}

func (c *S3Client) UploadStream(ctx context.Context, key string, data io.Reader) error {
	req := &s3.PutObjectInput{Bucket: &c.bucket, Key: &key, Body: data}
	res, err := c.uploader.Upload(ctx, req)
	if err != nil {
		return err
	}

	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("r2.etag", aws.ToString(res.ETag)),
		attribute.String("r2.versionId", aws.ToString(res.VersionID)),
		attribute.String("r2.key", key),
	)
	return nil
}

// DownloadStream downloads a map and returns an io.ReadCloser that the caller is responsible for closing.
func (c *S3Client) DownloadStream(ctx context.Context, key string) (io.ReadCloser, error) {
	req := &s3.GetObjectInput{Bucket: &c.bucket, Key: &key}
	res, err := c.rawClient.GetObject(ctx, req)
	if err != nil {
		var noKey *types.NoSuchKey
		if errors.As(err, &noKey) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Return the Body directly - it's an io.ReadCloser
	// Caller is responsible for closing it
	return res.Body, nil // Body is an io.ReadCloser. The caller is responsible for closing it
}

func (c *S3Client) Stat(ctx context.Context, key string) (*Info, error) {
	req := &s3.HeadObjectInput{Bucket: &c.bucket, Key: &key}
	res, err := c.rawClient.HeadObject(ctx, req)
	if err != nil {
		return nil, err
	}
	return &Info{Size: *res.ContentLength}, nil
}

func (c *S3Client) Delete(ctx context.Context, key string) error {
	req := &s3.DeleteObjectInput{Bucket: &c.bucket, Key: &key}
	_, err := c.rawClient.DeleteObject(ctx, req)
	return err
}

func (c *S3Client) Clone(ctx context.Context, src, dst string) error {
	srcKey := fmt.Sprintf("%s/%s", c.bucket, src)
	req := &s3.CopyObjectInput{Bucket: &c.bucket, CopySource: &srcKey, Key: &dst}
	_, err := c.rawClient.CopyObject(ctx, req)
	return err
}
