package v4Internal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/hollow-cube/api-server/config"
	"github.com/hollow-cube/api-server/internal/object"
	"github.com/hollow-cube/api-server/pkg/ox"
	"github.com/stretchr/testify/require"
)

type replayMemoryObjectClient struct {
	objects map[string][]byte
	mu      sync.RWMutex
}

func newReplayMemoryObjectClient() *replayMemoryObjectClient {
	return &replayMemoryObjectClient{objects: make(map[string][]byte)}
}

func (c *replayMemoryObjectClient) Upload(_ context.Context, key string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.objects[key] = bytes.Clone(data)
	return nil
}

func (c *replayMemoryObjectClient) Download(_ context.Context, key string) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, ok := c.objects[key]
	if !ok {
		return nil, object.ErrNotFound
	}
	return bytes.Clone(data), nil
}

func (c *replayMemoryObjectClient) UploadStream(_ context.Context, key string, data io.Reader) error {
	body, err := io.ReadAll(data)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.objects[key] = body
	return nil
}

func (c *replayMemoryObjectClient) DownloadStream(_ context.Context, key string) (io.ReadCloser, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, ok := c.objects[key]
	if !ok {
		return nil, object.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (c *replayMemoryObjectClient) DownloadRange(_ context.Context, key string, start, endInclusive int64) (io.ReadCloser, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, ok := c.objects[key]
	if !ok {
		return nil, object.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data[start : endInclusive+1])), nil
}

func (c *replayMemoryObjectClient) Stat(_ context.Context, key string) (*object.Info, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, ok := c.objects[key]
	if !ok {
		return nil, object.ErrNotFound
	}
	return &object.Info{Size: int64(len(data))}, nil
}

func (c *replayMemoryObjectClient) Delete(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.objects, key)
	return nil
}

func (c *replayMemoryObjectClient) Clone(_ context.Context, src, dst string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.objects[src]
	if !ok {
		return object.ErrNotFound
	}
	c.objects[dst] = bytes.Clone(data)
	return nil
}

func TestStageReplayCommit(t *testing.T) {
	preamble := []byte{1, 2, 3}
	segment := []byte{4, 5, 6, 7}
	body := append(bytes.Clone(preamble), segment...)
	bucket := newReplayMemoryObjectClient()
	server := &Server{
		replayBucket: bucket,
		replayLimits: config.Replay{
			MaxPreambleBytes:  16,
			MaxSegmentBytes:   16,
			MaxCommitBytes:    32,
			MaxCompactedBytes: 32,
		},
	}

	staged, err := server.stageReplayCommit(context.Background(), CommitReplayRequest{
		ReplayID:       "other-use-case",
		IfNoneMatch:    "*",
		IdempotencyKey: "commit-key",
		PreambleLength: "3",
		SegmentIndex:   "0",
		Final:          "false",
		ContentDigest:  contentDigestHeader(sha256Bytes(body)),
		Body: &ox.Stream{
			ContentType:   replayCommitContentType,
			ContentLength: int64(len(body)),
			Body:          bytes.NewReader(body),
		},
	})
	require.NoError(t, err)
	require.True(t, staged.creating)
	require.False(t, staged.final)
	require.Equal(t, preamble, staged.preamble)
	require.Equal(t, 0, *staged.segmentIndex)
	require.Equal(t, len(segment), staged.segmentLength)
	storedSegment, err := bucket.Download(context.Background(), staged.segmentObject)
	require.NoError(t, err)
	require.Equal(t, segment, storedSegment)
	require.Equal(t, sha256Bytes(segment), staged.segmentDigest)
	require.Len(t, staged.fingerprint, 32)
}

func TestStageReplayCommitInlinesSmallSegments(t *testing.T) {
	preamble := []byte{1, 2, 3}
	tests := []struct {
		name       string
		segment    []byte
		wantInline bool
	}{
		{name: "below threshold", segment: bytes.Repeat([]byte{4}, 7), wantInline: true},
		{name: "at threshold", segment: bytes.Repeat([]byte{4}, 8), wantInline: true},
		{name: "just over threshold", segment: bytes.Repeat([]byte{4}, 9)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := append(bytes.Clone(preamble), tt.segment...)
			bucket := newReplayMemoryObjectClient()
			server := &Server{
				replayBucket: bucket,
				replayLimits: config.Replay{
					MaxPreambleBytes:      16,
					MaxSegmentBytes:       64,
					MaxCommitBytes:        128,
					MaxInlineSegmentBytes: 8,
				},
			}

			staged, err := server.stageReplayCommit(context.Background(), CommitReplayRequest{
				ReplayID:       "replay",
				IfNoneMatch:    "*",
				IdempotencyKey: "commit-key",
				PreambleLength: "3",
				SegmentIndex:   "0",
				ContentDigest:  contentDigestHeader(sha256Bytes(body)),
				Body: &ox.Stream{
					ContentType:   replayCommitContentType,
					ContentLength: int64(len(body)),
					Body:          bytes.NewReader(body),
				},
			})
			require.NoError(t, err)
			require.Equal(t, len(tt.segment), staged.segmentLength)
			require.Equal(t, sha256Bytes(tt.segment), staged.segmentDigest)

			if tt.wantInline {
				require.Equal(t, tt.segment, staged.segmentData)
				require.Empty(t, staged.segmentObject)
				require.Empty(t, bucket.objects)
				return
			}

			require.Nil(t, staged.segmentData)
			require.NotEmpty(t, staged.segmentObject)
			stored, err := bucket.Download(context.Background(), staged.segmentObject)
			require.NoError(t, err)
			require.Equal(t, tt.segment, stored)
		})
	}
}

func TestStageReplayCommitRejectsDigestMismatchInline(t *testing.T) {
	body := []byte{1, 2, 3}
	bucket := newReplayMemoryObjectClient()
	server := &Server{
		replayBucket: bucket,
		replayLimits: config.Replay{
			MaxPreambleBytes:      16,
			MaxSegmentBytes:       16,
			MaxCommitBytes:        32,
			MaxInlineSegmentBytes: 8,
		},
	}

	_, err := server.stageReplayCommit(context.Background(), CommitReplayRequest{
		ReplayID:       "replay",
		IfNoneMatch:    "*",
		IdempotencyKey: "commit-key",
		PreambleLength: "1",
		SegmentIndex:   "0",
		ContentDigest:  contentDigestHeader(make([]byte, 32)),
		Body: &ox.Stream{
			ContentType:   replayCommitContentType,
			ContentLength: int64(len(body)),
			Body:          bytes.NewReader(body),
		},
	})
	var mismatch replayDigestMismatchError
	require.ErrorAs(t, err, &mismatch)
	require.Empty(t, bucket.objects)
}

func TestStageReplayCommitMetadataOnlyMustBeFinal(t *testing.T) {
	preamble := []byte{1, 2, 3}
	server := &Server{
		replayBucket: newReplayMemoryObjectClient(),
		replayLimits: config.Replay{
			MaxPreambleBytes: 16,
			MaxSegmentBytes:  16,
			MaxCommitBytes:   32,
		},
	}

	_, err := server.stageReplayCommit(context.Background(), CommitReplayRequest{
		ReplayID:       "replay",
		IfMatch:        `"r1"`,
		IdempotencyKey: "commit-key",
		PreambleLength: "3",
		Final:          "false",
		ContentDigest:  contentDigestHeader(sha256Bytes(preamble)),
		Body: &ox.Stream{
			ContentType:   replayCommitContentType,
			ContentLength: int64(len(preamble)),
			Body:          bytes.NewReader(preamble),
		},
	})
	require.Error(t, err)
	var badRequest ox.BadRequest
	require.ErrorAs(t, err, &badRequest)
}

func TestStageReplayCommitRejectsDigestMismatch(t *testing.T) {
	body := []byte{1, 2}
	server := &Server{
		replayBucket: newReplayMemoryObjectClient(),
		replayLimits: config.Replay{
			MaxPreambleBytes: 16,
			MaxSegmentBytes:  16,
			MaxCommitBytes:   32,
		},
	}

	_, err := server.stageReplayCommit(context.Background(), CommitReplayRequest{
		ReplayID:       "replay",
		IfNoneMatch:    "*",
		IdempotencyKey: "commit-key",
		PreambleLength: "1",
		SegmentIndex:   "0",
		ContentDigest:  contentDigestHeader(make([]byte, 32)),
		Body: &ox.Stream{
			ContentType:   replayCommitContentType,
			ContentLength: int64(len(body)),
			Body:          bytes.NewReader(body),
		},
	})
	var mismatch replayDigestMismatchError
	require.ErrorAs(t, err, &mismatch)
}

func TestStageCompactedReplayRetainsPreambleAndWholeObject(t *testing.T) {
	recording := []byte{1, 2, 3, 4, 5, 6}
	bucket := newReplayMemoryObjectClient()
	server := &Server{
		replayBucket: bucket,
		replayLimits: config.Replay{
			MaxPreambleBytes:  16,
			MaxCompactedBytes: 32,
		},
	}

	staged, err := server.stageCompactedReplay(context.Background(), PublishCompactedReplayRequest{
		ReplayID:       "replay",
		IfMatch:        `"r2"`,
		IdempotencyKey: "compact-key",
		PreambleLength: "4",
		ContentDigest:  contentDigestHeader(sha256Bytes(recording)),
		Body: &ox.Stream{
			ContentType:   replayContentType,
			ContentLength: int64(len(recording)),
			Body:          bytes.NewReader(recording),
		},
	})
	require.NoError(t, err)
	require.Equal(t, recording[:4], staged.preamble)
	storedRecording, err := bucket.Download(context.Background(), staged.object)
	require.NoError(t, err)
	require.Equal(t, recording, storedRecording)
	require.Equal(t, len(recording), staged.length)
	require.Equal(t, sha256Bytes(recording), staged.bodyDigest)
}

func TestParseReplayRange(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		length  int64
		start   int64
		end     int64
		partial bool
		wantErr bool
	}{
		{name: "full", length: 100, start: 0, end: 99},
		{name: "bounded", header: "bytes=10-19", length: 100, start: 10, end: 19, partial: true},
		{name: "open ended", header: "bytes=90-", length: 100, start: 90, end: 99, partial: true},
		{name: "suffix", header: "bytes=-10", length: 100, start: 90, end: 99, partial: true},
		{name: "clamps end", header: "bytes=90-200", length: 100, start: 90, end: 99, partial: true},
		{name: "start past end", header: "bytes=100-", length: 100, wantErr: true},
		{name: "multiple unsupported", header: "bytes=0-1,3-4", length: 100, wantErr: true},
		{name: "range on empty", header: "bytes=0-0", length: 0, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, partial, err := parseReplayRange(tt.header, tt.length)
			if tt.wantErr {
				require.Error(t, err)
				var rangeErr replayRangeError
				require.True(t, errors.As(err, &rangeErr))
				require.Equal(t, tt.length, rangeErr.length)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.start, start)
			require.Equal(t, tt.end, end)
			require.Equal(t, tt.partial, partial)
		})
	}
}

func TestReplayRequestFingerprintUsesSemanticSegmentIndex(t *testing.T) {
	bodyDigest := sha256Bytes([]byte("body"))
	first := replayRequestFingerprint("PATCH", "replay", `"r1"`, "", "3", "0", "false", bodyDigest)
	second := replayRequestFingerprint("PATCH", "replay", `"r1"`, "", "3", "0", "false", bodyDigest)
	different := replayRequestFingerprint("PATCH", "replay", `"r1"`, "", "3", "1", "false", bodyDigest)

	require.Equal(t, first, second)
	require.NotEqual(t, first, different)
}
