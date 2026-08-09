package v4Internal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/hollow-cube/api-server/config"
	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/metric"
	"github.com/hollow-cube/api-server/pkg/ox"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestReplayHTTPStorageIntegration exercises the replay endpoints against a
// real postgres. Point MAPDB_TEST_DATABASE_URL at an empty database; the
// mapdb migrations are applied automatically.
func TestReplayHTTPStorageIntegration(t *testing.T) {
	databaseURL := os.Getenv("MAPDB_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("MAPDB_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	store, pool, err := mapdb.NewQuerySet(ctx, metric.NewWriterNoop(zap.NewNop().Sugar()), databaseURL)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	_, err = pool.Exec(ctx, "truncate replay_idempotency, replay_segments, replays")
	require.NoError(t, err)

	bucket := newReplayMemoryObjectClient()
	server := &Server{
		mapStore:     store,
		replayBucket: bucket,
		replayLimits: config.Replay{
			MaxPreambleBytes:  1024,
			MaxSegmentBytes:   1024,
			MaxCommitBytes:    2048,
			MaxCompactedBytes: 4096,
		},
	}

	preamble := []byte{1, 2, 3}
	segment := []byte{4, 5, 6}
	firstRequest := replayCommitTestRequest("flow", "", "*", "first", 0, false, preamble, segment)
	firstResponse, err := server.CommitReplay(ctx, firstRequest)
	require.NoError(t, err)
	require.Equal(t, 201, firstResponse.StatusCode)
	require.Equal(t, `"r1"`, firstResponse.ETag)
	require.Equal(t, "recording", firstResponse.Headers["Replay-State"])
	require.Equal(t, "1", firstResponse.Headers["Replay-Next-Segment-Index"])

	retryResponse, err := server.CommitReplay(ctx, replayCommitTestRequest(
		"flow", "", "*", "first", 0, false, preamble, segment,
	))
	require.NoError(t, err)
	require.Equal(t, firstResponse.StatusCode, retryResponse.StatusCode)
	require.Equal(t, firstResponse.ETag, retryResponse.ETag)
	require.Equal(t, firstResponse.Headers, retryResponse.Headers)

	collisionRequest := replayCommitTestRequest("flow", "", "*", "first", 0, false, preamble, []byte{9})
	_, err = server.CommitReplay(ctx, collisionRequest)
	var idempotencyConflict replayIdempotencyConflictError
	require.ErrorAs(t, err, &idempotencyConflict)

	_, err = server.CommitReplay(ctx, replayCommitTestRequest("flow", `"stale"`, "", "stale", 1, false, preamble, []byte{7}))
	var preconditionFailed ox.PreconditionFailed
	require.ErrorAs(t, err, &preconditionFailed)

	finishedPreamble := []byte{8, 9, 10}
	finalResponse, err := server.CommitReplay(ctx, replayMetadataOnlyFinalTestRequest("flow", `"r1"`, "final", finishedPreamble))
	require.NoError(t, err)
	require.Equal(t, 200, finalResponse.StatusCode)
	require.Equal(t, `"r2"`, finalResponse.ETag)
	require.Equal(t, "finished", finalResponse.Headers["Replay-State"])

	replayResponse, err := server.GetReplay(ctx, ReplayRequest{ReplayID: "flow"})
	require.NoError(t, err)
	replayPreamble, err := io.ReadAll(replayResponse.Body)
	require.NoError(t, err)
	require.Equal(t, finishedPreamble, replayPreamble)
	require.Equal(t, `"r2"`, replayResponse.ETag)

	segmentResponse, err := server.GetReplayStream(ctx, ReplayStreamRequest{
		ReplayID: "flow",
		Segment:  "0",
		Range:    "bytes=1-2",
	})
	require.NoError(t, err)
	segmentRange, err := io.ReadAll(segmentResponse.Body)
	require.NoError(t, err)
	require.Equal(t, []byte{5, 6}, segmentRange)
	require.Equal(t, 206, segmentResponse.StatusCode)
	require.Equal(t, "bytes 1-2/3", segmentResponse.Headers["Content-Range"])

	compacted := []byte{11, 12, 13, 14, 15, 16}
	compactedResponse, err := server.PublishCompactedReplay(ctx, replayCompactionTestRequest(
		"flow", `"r2"`, "compact", 4, compacted,
	))
	require.NoError(t, err)
	require.Equal(t, `"r3"`, compactedResponse.ETag)
	require.Equal(t, "compacted", compactedResponse.Headers["Replay-Representation"])

	compactedPreambleResponse, err := server.GetReplay(ctx, ReplayRequest{ReplayID: "flow"})
	require.NoError(t, err)
	compactedPreamble, err := io.ReadAll(compactedPreambleResponse.Body)
	require.NoError(t, err)
	require.Equal(t, compacted[:4], compactedPreamble)
	require.Empty(t, compactedPreambleResponse.Headers["Replay-Next-Segment-Index"])

	compactedStream, err := server.GetReplayStream(ctx, ReplayStreamRequest{
		ReplayID: "flow",
		Range:    "bytes=2-4",
	})
	require.NoError(t, err)
	compactedRange, err := io.ReadAll(compactedStream.Body)
	require.NoError(t, err)
	require.Equal(t, compacted[2:5], compactedRange)
	require.Equal(t, 206, compactedStream.StatusCode)

	testReplayConcurrentWriters(t, ctx, server)
	testReplayInlineSegments(t, ctx, store, pool, bucket)
}

// testReplayInlineSegments covers a replay whose segments straddle the inline
// threshold: small ones live in replay_segments.data, larger ones keep going to
// object storage, and both read back identically.
func testReplayInlineSegments(t *testing.T, ctx context.Context, store *mapdb.Store, pool *pgxpool.Pool, bucket *replayMemoryObjectClient) {
	server := &Server{
		mapStore:     store,
		replayBucket: bucket,
		replayLimits: config.Replay{
			MaxPreambleBytes:      1024,
			MaxSegmentBytes:       1024,
			MaxCommitBytes:        2048,
			MaxCompactedBytes:     4096,
			MaxInlineSegmentBytes: 8,
		},
	}

	preamble := []byte{1, 2, 3}
	external := bytes.Repeat([]byte{9}, 9)
	inline := bytes.Repeat([]byte{7}, 8)

	// Segment 0 is over the threshold and goes to object storage, segment 1 is
	// at the threshold and is stored inline.
	externalResponse, err := server.CommitReplay(ctx, replayCommitTestRequest(
		"inline", "", "*", "inline-external", 0, false, preamble, external,
	))
	require.NoError(t, err)
	require.Equal(t, 201, externalResponse.StatusCode)

	inlineResponse, err := server.CommitReplay(ctx, replayCommitTestRequest(
		"inline", `"r1"`, "", "inline-small", 1, false, preamble, inline,
	))
	require.NoError(t, err)
	require.Equal(t, 200, inlineResponse.StatusCode)
	require.Equal(t, "2", inlineResponse.Headers["Replay-Next-Segment-Index"])

	externalRow, err := store.GetReplaySegment(ctx, "inline", 0)
	require.NoError(t, err)
	require.Nil(t, externalRow.Data)
	require.NotNil(t, externalRow.ObjectReference)
	storedObject, err := bucket.Download(ctx, *externalRow.ObjectReference)
	require.NoError(t, err)
	require.Equal(t, external, storedObject)

	inlineRow, err := store.GetReplaySegment(ctx, "inline", 1)
	require.NoError(t, err)
	require.Nil(t, inlineRow.ObjectReference)
	require.Equal(t, inline, inlineRow.Data)
	require.Equal(t, len(inline), inlineRow.Length)
	require.Equal(t, sha256Bytes(inline), inlineRow.Digest)

	// Both segments read back in order with identical semantics.
	for index, want := range [][]byte{external, inline} {
		response, err := server.GetReplayStream(ctx, ReplayStreamRequest{
			ReplayID: "inline",
			Segment:  strconv.Itoa(index),
		})
		require.NoError(t, err)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.Equal(t, want, body)
		require.Equal(t, 200, response.StatusCode)
		require.Equal(t, int64(len(want)), response.ContentLength)
		require.Equal(t, "application/octet-stream", response.ContentType)
		require.Equal(t, objectETag(sha256Bytes(want)), response.ETag)
		require.Equal(t, "bytes", response.Headers["Accept-Ranges"])
	}

	inlineRange, err := server.GetReplayStream(ctx, ReplayStreamRequest{
		ReplayID: "inline",
		Segment:  "1",
		Range:    "bytes=2-4",
	})
	require.NoError(t, err)
	inlineRangeBody, err := io.ReadAll(inlineRange.Body)
	require.NoError(t, err)
	require.Equal(t, inline[2:5], inlineRangeBody)
	require.Equal(t, 206, inlineRange.StatusCode)
	require.Equal(t, int64(3), inlineRange.ContentLength)
	require.Equal(t, "bytes 2-4/8", inlineRange.Headers["Content-Range"])
	require.Equal(t, objectETag(sha256Bytes(inline)), inlineRange.ETag)

	inlineSuffix, err := server.GetReplayStream(ctx, ReplayStreamRequest{
		ReplayID: "inline",
		Segment:  "1",
		Range:    "bytes=-3",
	})
	require.NoError(t, err)
	inlineSuffixBody, err := io.ReadAll(inlineSuffix.Body)
	require.NoError(t, err)
	require.Equal(t, inline[5:], inlineSuffixBody)
	require.Equal(t, "bytes 5-7/8", inlineSuffix.Headers["Content-Range"])

	// A retried commit on either path returns the recorded response and does
	// not write a second segment row.
	for _, retry := range []struct {
		key      string
		index    int
		ifMatch  string
		expected *ox.Stream
		segment  []byte
	}{
		{key: "inline-external", index: 0, ifMatch: "", expected: externalResponse, segment: external},
		{key: "inline-small", index: 1, ifMatch: `"r1"`, expected: inlineResponse, segment: inline},
	} {
		ifNoneMatch := ""
		if retry.ifMatch == "" {
			ifNoneMatch = "*"
		}
		response, err := server.CommitReplay(ctx, replayCommitTestRequest(
			"inline", retry.ifMatch, ifNoneMatch, retry.key, retry.index, false, preamble, retry.segment,
		))
		require.NoError(t, err)
		require.Equal(t, retry.expected.StatusCode, response.StatusCode)
		require.Equal(t, retry.expected.ETag, response.ETag)
		require.Equal(t, retry.expected.Headers, response.Headers)
	}

	var segmentCount int
	require.NoError(t, pool.QueryRow(ctx, "select count(*) from replay_segments where replay_id = 'inline'").Scan(&segmentCount))
	require.Equal(t, 2, segmentCount)

	var version int
	require.NoError(t, pool.QueryRow(ctx, "select version from replays where id = 'inline'").Scan(&version))
	require.Equal(t, 2, version)

	// Digest mismatches are rejected before anything is persisted on either path.
	for _, mismatch := range [][]byte{inline, external} {
		request := replayCommitTestRequest("inline", `"r2"`, "", "inline-mismatch", 2, false, preamble, mismatch)
		request.ContentDigest = contentDigestHeader(make([]byte, 32))
		_, err := server.CommitReplay(ctx, request)
		var digestMismatch replayDigestMismatchError
		require.ErrorAs(t, err, &digestMismatch)
	}
	require.NoError(t, pool.QueryRow(ctx, "select count(*) from replay_segments where replay_id = 'inline'").Scan(&segmentCount))
	require.Equal(t, 2, segmentCount)
}

func testReplayConcurrentWriters(t *testing.T, ctx context.Context, server *Server) {
	_, err := server.CommitReplay(ctx, replayCommitTestRequest(
		"race", "", "*", "race-first", 0, false, []byte{1}, []byte{2},
	))
	require.NoError(t, err)

	requests := []CommitReplayRequest{
		replayCommitTestRequest("race", `"r1"`, "", "race-a", 1, false, []byte{3}, []byte{4}),
		replayCommitTestRequest("race", `"r1"`, "", "race-b", 1, false, []byte{5}, []byte{6}),
	}
	results := make(chan error, len(requests))
	var wg sync.WaitGroup
	for _, request := range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := server.CommitReplay(ctx, request)
			results <- err
		}()
	}
	wg.Wait()
	close(results)

	var successes, preconditionFailures int
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		var preconditionFailed ox.PreconditionFailed
		if errors.As(err, &preconditionFailed) {
			preconditionFailures++
			continue
		}
		require.NoError(t, err)
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, preconditionFailures)
}

func replayCommitTestRequest(
	replayID string,
	ifMatch string,
	ifNoneMatch string,
	idempotencyKey string,
	segmentIndex int,
	final bool,
	preamble []byte,
	segment []byte,
) CommitReplayRequest {
	body := append(bytes.Clone(preamble), segment...)
	return CommitReplayRequest{
		ReplayID:       replayID,
		IfMatch:        ifMatch,
		IfNoneMatch:    ifNoneMatch,
		IdempotencyKey: idempotencyKey,
		PreambleLength: strconv.Itoa(len(preamble)),
		SegmentIndex:   strconv.Itoa(segmentIndex),
		Final:          strconv.FormatBool(final),
		ContentDigest:  contentDigestHeader(sha256Bytes(body)),
		Body: &ox.Stream{
			ContentType:   replayCommitContentType,
			ContentLength: int64(len(body)),
			Body:          bytes.NewReader(body),
		},
	}
}

func replayMetadataOnlyFinalTestRequest(replayID, ifMatch, idempotencyKey string, preamble []byte) CommitReplayRequest {
	return CommitReplayRequest{
		ReplayID:       replayID,
		IfMatch:        ifMatch,
		IdempotencyKey: idempotencyKey,
		PreambleLength: strconv.Itoa(len(preamble)),
		Final:          "true",
		ContentDigest:  contentDigestHeader(sha256Bytes(preamble)),
		Body: &ox.Stream{
			ContentType:   replayCommitContentType,
			ContentLength: int64(len(preamble)),
			Body:          bytes.NewReader(preamble),
		},
	}
}

func replayCompactionTestRequest(
	replayID string,
	ifMatch string,
	idempotencyKey string,
	preambleLength int,
	recording []byte,
) PublishCompactedReplayRequest {
	return PublishCompactedReplayRequest{
		ReplayID:       replayID,
		IfMatch:        ifMatch,
		IdempotencyKey: idempotencyKey,
		PreambleLength: strconv.Itoa(preambleLength),
		ContentDigest:  contentDigestHeader(sha256Bytes(recording)),
		Body: &ox.Stream{
			ContentType:   replayContentType,
			ContentLength: int64(len(recording)),
			Body:          bytes.NewReader(recording),
		},
	}
}
