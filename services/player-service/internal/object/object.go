package object

import (
	"context"
	"errors"
	"io"
)

var ErrNotFound = errors.New("not found")

type Info struct {
	Size int64
}

type Client interface {
	Upload(ctx context.Context, key string, data []byte) error
	Download(ctx context.Context, key string) ([]byte, error)

	UploadStream(ctx context.Context, key string, data io.Reader) error
	DownloadStream(ctx context.Context, key string) (io.ReadCloser, error)

	Stat(ctx context.Context, key string) (*Info, error)

	Delete(ctx context.Context, key string) error
	Clone(ctx context.Context, src, dst string) error
}
