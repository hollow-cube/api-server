package object

import (
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"go.uber.org/fx"
	"go.uber.org/zap"
	"io"
)

var _ Client = (*S3Client)(nil)

type S3Client struct {
	log        *zap.SugaredLogger
	rawClient  *s3.S3
	downloader *s3manager.Downloader
	uploader   *s3manager.Uploader
	bucket     string
}

type S3ClientParams struct {
	fx.In
	Log        *zap.SugaredLogger
	RawClient  *s3.S3
	Downloader *s3manager.Downloader
	Uploader   *s3manager.Uploader
}

func NewS3ClientFactory(bucket string) any {
	return func(p S3ClientParams) (Client, error) {
		buckets, err := p.RawClient.ListBuckets(&s3.ListBucketsInput{})
		if err != nil {
			return nil, fmt.Errorf("failed to list buckets: %w", err)
		}

		var found bool
		for _, b := range buckets.Buckets {
			if *b.Name == bucket {
				found = true
				break
			}
		}
		if !found {
			_, err := p.RawClient.CreateBucket(&s3.CreateBucketInput{Bucket: &bucket})
			if err != nil {
				return nil, fmt.Errorf("failed to create bucket: %w", err)
			}
		}

		return &S3Client{
			log:        p.Log.With("object", "s3"),
			rawClient:  p.RawClient,
			downloader: p.Downloader,
			uploader:   p.Uploader,
			bucket:     bucket,
		}, nil
	}
}

func (c *S3Client) Upload(ctx context.Context, key string, data []byte) error {
	req := &s3manager.UploadInput{Bucket: &c.bucket, Key: &key, Body: bytes.NewReader(data)}
	_, err := c.uploader.UploadWithContext(ctx, req)
	return err
}

func (c *S3Client) Download(ctx context.Context, key string) ([]byte, error) {
	var b aws.WriteAtBuffer
	req := &s3.GetObjectInput{Bucket: &c.bucket, Key: &key}
	_, err := c.downloader.DownloadWithContext(ctx, &b, req)
	if err != nil {
		if s3Err, ok := err.(awserr.Error); ok && s3Err.Code() == s3.ErrCodeNoSuchKey {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return b.Bytes(), nil
}

func (c *S3Client) UploadStream(ctx context.Context, key string, data io.Reader) error {
	req := &s3manager.UploadInput{Bucket: &c.bucket, Key: &key, Body: data}
	_, err := c.uploader.UploadWithContext(ctx, req)
	return err
}

func (c *S3Client) DownloadStream(ctx context.Context, key string) (io.Reader, error) {
	var b aws.WriteAtBuffer
	req := &s3.GetObjectInput{Bucket: &c.bucket, Key: &key}
	_, err := c.downloader.DownloadWithContext(ctx, &b, req)
	if err != nil {
		if s3Err, ok := err.(awserr.Error); ok && s3Err.Code() == s3.ErrCodeNoSuchKey {
			return nil, ErrNotFound
		}
		return nil, err
	}
	//todo how to stream
	return bytes.NewReader(b.Bytes()), nil
}

func (c *S3Client) Stat(ctx context.Context, key string) (*Info, error) {
	req := &s3.HeadObjectInput{Bucket: &c.bucket, Key: &key}
	res, err := c.rawClient.HeadObjectWithContext(ctx, req)
	if err != nil {
		return nil, err
	}
	return &Info{Size: *res.ContentLength}, nil
}

func (c *S3Client) Delete(ctx context.Context, key string) error {
	req := &s3.DeleteObjectInput{Bucket: &c.bucket, Key: &key}
	_, err := c.rawClient.DeleteObjectWithContext(ctx, req)
	return err
}

func (c *S3Client) Clone(ctx context.Context, src, dst string) error {
	srcKey := fmt.Sprintf("%s/%s", c.bucket, src)
	req := &s3.CopyObjectInput{Bucket: &c.bucket, CopySource: &srcKey, Key: &dst}
	_, err := c.rawClient.CopyObjectWithContext(ctx, req)
	return err
}
