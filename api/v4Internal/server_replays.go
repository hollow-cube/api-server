package v4Internal

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/object"
	"github.com/hollow-cube/api-server/pkg/ox"
)

const (
	replayPreambleContentType = "application/vnd.hollowcube.replay-preamble"
	replayCommitContentType   = "application/vnd.hollowcube.replay-commit"
	replayContentType         = "application/vnd.hollowcube.replay"
)

type ReplayRequest struct {
	ReplayID string `path:"id"`
}

type ReplayStreamRequest struct {
	ReplayID string `path:"id"`
	Segment  string `query:"segment,optional"`
	Range    string `header:"Range,optional"`
}

type CommitReplayRequest struct {
	ReplayID       string `path:"id"`
	IfMatch        string `header:"If-Match,optional"`
	IfNoneMatch    string `header:"If-None-Match,optional"`
	IdempotencyKey string `header:"Idempotency-Key"`
	PreambleLength string `header:"Replay-Preamble-Length"`
	SegmentIndex   string `header:"Replay-Segment-Index,optional"`
	Final          string `header:"Replay-Final,optional"`
	ContentDigest  string `header:"Content-Digest"`
	Body           *ox.Stream
}

type PublishCompactedReplayRequest struct {
	ReplayID       string `path:"id"`
	IfMatch        string `header:"If-Match"`
	IdempotencyKey string `header:"Idempotency-Key"`
	PreambleLength string `header:"Replay-Preamble-Length"`
	ContentDigest  string `header:"Content-Digest"`
	Body           *ox.Stream
}

type replayResponseMetadata struct {
	State            string `json:"state"`
	Representation   string `json:"representation"`
	NextSegmentIndex *int   `json:"nextSegmentIndex,omitempty"`
}

type stagedReplayCommit struct {
	preamble       []byte
	preambleDigest []byte
	bodyDigest     []byte
	segmentIndex   *int
	segmentObject  string // empty when the segment is stored inline
	segmentData    []byte // nil when the segment is stored in object storage
	segmentLength  int
	segmentDigest  []byte
	final          bool
	fingerprint    []byte
	creating       bool
}

type stagedCompactedReplay struct {
	preamble       []byte
	preambleDigest []byte
	bodyDigest     []byte
	object         string
	length         int
	fingerprint    []byte
}

type replayNotCompactedError struct {
	ox.Conflict
}

func (replayNotCompactedError) Error() string { return "replay_not_compacted" }

type replayFinishedError struct {
	ox.Conflict
}

func (replayFinishedError) Error() string { return "replay_finished" }

type replayNotFinishedError struct {
	ox.Conflict
}

func (replayNotFinishedError) Error() string { return "replay_not_finished" }

type replayWrongSegmentError struct {
	ox.Conflict
}

func (replayWrongSegmentError) Error() string { return "wrong_segment_index" }

type replayIdempotencyConflictError struct {
	ox.Conflict
}

func (replayIdempotencyConflictError) Error() string { return "idempotency_key_conflict" }

type replayDigestMismatchError struct {
	ox.ValidationError
}

func (replayDigestMismatchError) Error() string { return "digest_mismatch" }

type replayRangeError struct {
	ox.RangeNotSatisfiable
	length int64
}

func (e replayRangeError) Headers() map[string]string {
	return map[string]string{"Content-Range": fmt.Sprintf("bytes */%d", e.length)}
}

// GET /replays/{id}
// ox:produces application/vnd.hollowcube.replay-preamble
func (s *Server) GetReplay(ctx context.Context, request ReplayRequest) (*ox.Stream, error) {
	replay, err := s.mapStore.GetReplay(ctx, request.ReplayID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return nil, ox.NotFound{}
	} else if err != nil {
		return nil, fmt.Errorf("failed to get replay: %w", err)
	}
	if err := validateStoredReplay(replay); err != nil {
		return nil, err
	}

	headers := replayStateHeaders(replayMetadata(replay))
	headers["Content-Digest"] = contentDigestHeader(replay.CurrentPreambleDigest)

	return &ox.Stream{
		ContentType:   replayPreambleContentType,
		Body:          bytes.NewReader(replay.CurrentPreamble),
		ContentLength: int64(len(replay.CurrentPreamble)),
		ETag:          replayETag(replay.Version),
		Headers:       headers,
	}, nil
}

// GET /replays/{id}/stream
// ox:produces application/octet-stream, application/vnd.hollowcube.replay
// ox:success 200, 206
func (s *Server) GetReplayStream(ctx context.Context, request ReplayStreamRequest) (*ox.Stream, error) {
	replay, err := s.mapStore.GetReplay(ctx, request.ReplayID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return nil, ox.NotFound{}
	} else if err != nil {
		return nil, fmt.Errorf("failed to get replay: %w", err)
	}
	if err := validateStoredReplay(replay); err != nil {
		return nil, err
	}

	var objectReference, contentType string
	var inlineData []byte
	var length int64
	var digest []byte
	if request.Segment != "" {
		segmentIndex, err := parseNonnegativeInt(request.Segment)
		if err != nil {
			return nil, err
		}
		segment, err := s.mapStore.GetReplaySegment(ctx, request.ReplayID, segmentIndex)
		if errors.Is(err, mapdb.ErrNoRows) {
			return nil, ox.NotFound{}
		} else if err != nil {
			return nil, fmt.Errorf("failed to get replay segment: %w", err)
		}
		// Branch on which column is populated rather than on the current
		// inline threshold, so changing the threshold never misroutes a
		// segment written under an older one.
		switch {
		case segment.Data != nil:
			if len(segment.Data) != segment.Length {
				return nil, fmt.Errorf("stored replay %q segment %d has inconsistent inline length", replay.ID, segment.SegmentIndex)
			}
			inlineData = segment.Data
		case segment.ObjectReference != nil && *segment.ObjectReference != "":
			objectReference = *segment.ObjectReference
		default:
			return nil, fmt.Errorf("stored replay %q segment %d has no data", replay.ID, segment.SegmentIndex)
		}
		contentType = "application/octet-stream"
		length = int64(segment.Length)
		digest = segment.Digest
	} else {
		if replay.Representation != "compacted" {
			return nil, replayNotCompactedError{}
		}
		if replay.CompactedObject == nil || replay.CompactedLength == nil || replay.CompactedDigest == nil {
			return nil, fmt.Errorf("stored replay %q has incomplete compacted state", replay.ID)
		}
		objectReference = *replay.CompactedObject
		contentType = replayContentType
		length = int64(*replay.CompactedLength)
		digest = replay.CompactedDigest
	}

	start, end, partial, err := parseReplayRange(request.Range, length)
	if err != nil {
		return nil, err
	}

	var body io.ReadCloser
	if inlineData != nil {
		slice := inlineData
		if partial {
			slice = inlineData[start : end+1]
		}
		body = io.NopCloser(bytes.NewReader(slice))
	} else {
		if partial {
			body, err = s.replayBucket.DownloadRange(ctx, objectReference, start, end)
		} else {
			body, err = s.replayBucket.DownloadStream(ctx, objectReference)
		}
		if errors.Is(err, object.ErrNotFound) {
			return nil, fmt.Errorf("stored replay object %q is missing: %w", objectReference, err)
		} else if err != nil {
			return nil, fmt.Errorf("failed to download replay object: %w", err)
		}
	}

	responseLength := length
	status := http.StatusOK
	headers := map[string]string{
		"Accept-Ranges": "bytes",
	}
	if partial {
		responseLength = end - start + 1
		status = http.StatusPartialContent
		headers["Content-Range"] = fmt.Sprintf("bytes %d-%d/%d", start, end, length)
	}

	return &ox.Stream{
		ContentType:   contentType,
		Body:          body,
		ContentLength: responseLength,
		ETag:          objectETag(digest),
		Headers:       headers,
		StatusCode:    status,
	}, nil
}

// PATCH /replays/{id}/stream
// ox:consumes application/vnd.hollowcube.replay-commit
// ox:produces application/octet-stream
// ox:success 200, 201
func (s *Server) CommitReplay(ctx context.Context, request CommitReplayRequest) (*ox.Stream, error) {
	request.IfMatch = strings.TrimSpace(request.IfMatch)
	request.IfNoneMatch = strings.TrimSpace(request.IfNoneMatch)
	staged, err := s.stageReplayCommit(ctx, request)
	if err != nil {
		return nil, err
	}

	return mapdb.Tx(ctx, s.mapStore, func(ctx context.Context, tx *mapdb.Store) (*ox.Stream, error) {
		replay, err := tx.GetReplayForUpdate(ctx, request.ReplayID)
		if errors.Is(err, mapdb.ErrNoRows) {
			if !staged.creating {
				return nil, ox.PreconditionFailed{}
			}
			if staged.segmentIndex != nil && *staged.segmentIndex != 0 {
				return nil, replayWrongSegmentError{}
			}

			replay, err = tx.CreateReplayIfAbsent(ctx, mapdb.CreateReplayIfAbsentParams{
				ID:                    request.ReplayID,
				State:                 replayState(staged.final),
				NextSegmentIndex:      nextReplaySegmentIndex(0, staged.segmentIndex),
				CurrentPreamble:       staged.preamble,
				CurrentPreambleDigest: staged.preambleDigest,
			})
			if errors.Is(err, mapdb.ErrNoRows) {
				// Another first commit won the insert race. Lock and evaluate
				// its idempotency/precondition state like any existing replay.
				replay, err = tx.GetReplayForUpdate(ctx, request.ReplayID)
			} else if err != nil {
				return nil, fmt.Errorf("failed to create replay: %w", err)
			} else {
				return finishReplayCommit(ctx, tx, request, staged, replay, http.StatusCreated)
			}
		}
		if err != nil {
			return nil, fmt.Errorf("failed to lock replay: %w", err)
		}

		if response, found, err := replayIdempotentResponse(ctx, tx, request.ReplayID, request.IdempotencyKey, staged.fingerprint); err != nil {
			return nil, err
		} else if found {
			return response, nil
		}
		if staged.creating || request.IfMatch != replayETag(replay.Version) {
			return nil, ox.PreconditionFailed{}
		}
		if replay.State == "finished" {
			return nil, replayFinishedError{}
		}
		if staged.segmentIndex != nil && *staged.segmentIndex != replay.NextSegmentIndex {
			return nil, replayWrongSegmentError{}
		}

		replay, err = tx.UpdateReplayRecording(ctx, mapdb.UpdateReplayRecordingParams{
			State:                 replayState(staged.final),
			NextSegmentIndex:      nextReplaySegmentIndex(replay.NextSegmentIndex, staged.segmentIndex),
			CurrentPreamble:       staged.preamble,
			CurrentPreambleDigest: staged.preambleDigest,
			ID:                    request.ReplayID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to update replay: %w", err)
		}

		return finishReplayCommit(ctx, tx, request, staged, replay, http.StatusOK)
	})
}

// PUT /replays/{id}/stream
// ox:consumes application/vnd.hollowcube.replay
// ox:produces application/octet-stream
func (s *Server) PublishCompactedReplay(ctx context.Context, request PublishCompactedReplayRequest) (*ox.Stream, error) {
	request.IfMatch = strings.TrimSpace(request.IfMatch)
	staged, err := s.stageCompactedReplay(ctx, request)
	if err != nil {
		return nil, err
	}

	return mapdb.Tx(ctx, s.mapStore, func(ctx context.Context, tx *mapdb.Store) (*ox.Stream, error) {
		replay, err := tx.GetReplayForUpdate(ctx, request.ReplayID)
		if errors.Is(err, mapdb.ErrNoRows) {
			return nil, ox.NotFound{}
		} else if err != nil {
			return nil, fmt.Errorf("failed to lock replay: %w", err)
		}

		if response, found, err := replayIdempotentResponse(ctx, tx, request.ReplayID, request.IdempotencyKey, staged.fingerprint); err != nil {
			return nil, err
		} else if found {
			return response, nil
		}
		if request.IfMatch != replayETag(replay.Version) {
			return nil, ox.PreconditionFailed{}
		}
		if replay.State != "finished" {
			return nil, replayNotFinishedError{}
		}

		replay, err = tx.PublishReplayCompacted(ctx, mapdb.PublishReplayCompactedParams{
			CurrentPreamble:       staged.preamble,
			CurrentPreambleDigest: staged.preambleDigest,
			CompactedObject:       &staged.object,
			CompactedLength:       &staged.length,
			CompactedDigest:       staged.bodyDigest,
			ID:                    request.ReplayID,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to publish compacted replay: %w", err)
		}

		metadata := replayMetadata(replay)
		response := replayWriteResponse(http.StatusOK, replayETag(replay.Version), metadata)
		if err := createReplayIdempotency(ctx, tx, request.ReplayID, request.IdempotencyKey, staged.fingerprint, response, metadata); err != nil {
			return nil, err
		}
		return response, nil
	})
}

func finishReplayCommit(
	ctx context.Context,
	tx *mapdb.Store,
	request CommitReplayRequest,
	staged *stagedReplayCommit,
	replay mapdb.Replay,
	status int,
) (*ox.Stream, error) {
	if staged.segmentIndex != nil {
		var objectReference *string
		if staged.segmentData == nil {
			objectReference = &staged.segmentObject
		}
		if err := tx.CreateReplaySegment(ctx, mapdb.CreateReplaySegmentParams{
			ReplayID:        request.ReplayID,
			SegmentIndex:    *staged.segmentIndex,
			ObjectReference: objectReference,
			Data:            staged.segmentData,
			Length:          staged.segmentLength,
			Digest:          staged.segmentDigest,
			CommitRevision:  replay.RecordingRevision,
		}); err != nil {
			return nil, fmt.Errorf("failed to install replay segment: %w", err)
		}
	}

	metadata := replayMetadata(replay)
	response := replayWriteResponse(status, replayETag(replay.Version), metadata)
	if err := createReplayIdempotency(ctx, tx, request.ReplayID, request.IdempotencyKey, staged.fingerprint, response, metadata); err != nil {
		return nil, err
	}
	return response, nil
}

func (s *Server) stageReplayCommit(ctx context.Context, request CommitReplayRequest) (*stagedReplayCommit, error) {
	if err := validateReplayIDAndKey(request.ReplayID, request.IdempotencyKey); err != nil {
		return nil, err
	}
	creating, err := parseReplayWritePrecondition(request.IfMatch, request.IfNoneMatch)
	if err != nil {
		return nil, err
	}
	preambleLength, err := parseReplayPreambleLength(request.PreambleLength, s.replayLimits.MaxPreambleBytes)
	if err != nil {
		return nil, err
	}
	final, err := parseOptionalBool(request.Final)
	if err != nil {
		return nil, err
	}
	expectedDigest, err := parseContentDigest(request.ContentDigest)
	if err != nil {
		return nil, err
	}
	if request.Body == nil || request.Body.Body == nil {
		return nil, ox.BadRequest{}
	}
	if !mediaTypeMatches(request.Body.ContentType, replayCommitContentType) {
		return nil, ox.BadRequest{}
	}
	if request.Body.ContentLength >= 0 {
		if request.Body.ContentLength > s.replayLimits.MaxCommitBytes {
			return nil, ox.ContentTooLarge{}
		}
		if request.Body.ContentLength < int64(preambleLength) {
			return nil, ox.BadRequest{}
		}
		if request.Body.ContentLength-int64(preambleLength) > s.replayLimits.MaxSegmentBytes {
			return nil, ox.ContentTooLarge{}
		}
	}

	preamble := make([]byte, preambleLength)
	if _, err := io.ReadFull(request.Body.Body, preamble); err != nil {
		return nil, ox.BadRequest{}
	}
	bodyHash := sha256.New()
	_, _ = bodyHash.Write(preamble)

	reader := bufio.NewReader(request.Body.Body)
	hasSegment := true
	if _, err := reader.Peek(1); errors.Is(err, io.EOF) {
		hasSegment = false
	} else if err != nil {
		return nil, fmt.Errorf("failed to read replay commit body: %w", err)
	}

	var segmentIndex *int
	var segmentObject string
	var segmentData []byte
	var segmentLength int
	var segmentDigest []byte
	if hasSegment {
		index, err := parseNonnegativeInt(request.SegmentIndex)
		if err != nil {
			return nil, err
		}
		segmentIndex = &index

		segmentHash := sha256.New()
		limited := &io.LimitedReader{R: reader, N: s.replayLimits.MaxSegmentBytes + 1}
		digestSink := io.MultiWriter(bodyHash, segmentHash)

		// Buffer one byte past the inline threshold: if the segment fits it is
		// written into the segment row inside the commit transaction, so there
		// is no object to orphan if the transaction fails. Anything larger is
		// streamed on to object storage from the buffered prefix.
		var head bytes.Buffer
		if _, err := io.CopyN(&head, limited, s.replayLimits.MaxInlineSegmentBytes+1); err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("failed to read replay commit body: %w", err)
		}
		if int64(head.Len()) <= s.replayLimits.MaxInlineSegmentBytes {
			segmentData = head.Bytes()
			_, _ = digestSink.Write(segmentData)
		} else {
			segmentObject = replayObjectKey(request.ReplayID, fmt.Sprintf("segments/%d", index))
			stream := io.TeeReader(io.MultiReader(&head, limited), digestSink)
			if err := s.replayBucket.UploadStream(ctx, segmentObject, stream); err != nil {
				return nil, fmt.Errorf("failed to stage replay segment: %w", err)
			}
		}
		segmentLength64 := s.replayLimits.MaxSegmentBytes + 1 - limited.N
		if segmentLength64 > s.replayLimits.MaxSegmentBytes {
			return nil, ox.ContentTooLarge{}
		}
		if segmentLength64+int64(preambleLength) > s.replayLimits.MaxCommitBytes {
			return nil, ox.ContentTooLarge{}
		}
		if request.Body.ContentLength >= 0 && request.Body.ContentLength != segmentLength64+int64(preambleLength) {
			return nil, ox.BadRequest{}
		}
		segmentLength = int(segmentLength64)
		segmentDigest = segmentHash.Sum(nil)
	} else {
		if request.SegmentIndex != "" {
			return nil, ox.BadRequest{}
		}
		if !final {
			return nil, ox.BadRequest{}
		}
		if request.Body.ContentLength >= 0 && request.Body.ContentLength != int64(preambleLength) {
			return nil, ox.BadRequest{}
		}
	}

	bodyDigest := bodyHash.Sum(nil)
	if !bytes.Equal(bodyDigest, expectedDigest) {
		return nil, replayDigestMismatchError{}
	}
	segmentIndexFingerprint := ""
	if segmentIndex != nil {
		segmentIndexFingerprint = strconv.Itoa(*segmentIndex)
	}
	fingerprint := replayRequestFingerprint(
		http.MethodPatch,
		request.ReplayID,
		request.IfMatch,
		request.IfNoneMatch,
		strconv.Itoa(preambleLength),
		segmentIndexFingerprint,
		strconv.FormatBool(final),
		bodyDigest,
	)

	return &stagedReplayCommit{
		preamble:       preamble,
		preambleDigest: sha256Bytes(preamble),
		bodyDigest:     bodyDigest,
		segmentIndex:   segmentIndex,
		segmentObject:  segmentObject,
		segmentData:    segmentData,
		segmentLength:  segmentLength,
		segmentDigest:  segmentDigest,
		final:          final,
		fingerprint:    fingerprint,
		creating:       creating,
	}, nil
}

func (s *Server) stageCompactedReplay(ctx context.Context, request PublishCompactedReplayRequest) (*stagedCompactedReplay, error) {
	if err := validateReplayIDAndKey(request.ReplayID, request.IdempotencyKey); err != nil {
		return nil, err
	}
	if request.IfMatch == "" {
		return nil, ox.BadRequest{}
	}
	preambleLength, err := parseReplayPreambleLength(request.PreambleLength, s.replayLimits.MaxPreambleBytes)
	if err != nil {
		return nil, err
	}
	expectedDigest, err := parseContentDigest(request.ContentDigest)
	if err != nil {
		return nil, err
	}
	if request.Body == nil || request.Body.Body == nil {
		return nil, ox.BadRequest{}
	}
	if !mediaTypeMatches(request.Body.ContentType, replayContentType) {
		return nil, ox.BadRequest{}
	}
	if request.Body.ContentLength >= 0 {
		if request.Body.ContentLength > s.replayLimits.MaxCompactedBytes {
			return nil, ox.ContentTooLarge{}
		}
		if request.Body.ContentLength < int64(preambleLength) {
			return nil, ox.BadRequest{}
		}
	}

	preamble := make([]byte, preambleLength)
	if _, err := io.ReadFull(request.Body.Body, preamble); err != nil {
		return nil, ox.BadRequest{}
	}

	objectReference := replayObjectKey(request.ReplayID, "compacted")
	bodyHash := sha256.New()
	limited := &io.LimitedReader{
		R: io.MultiReader(bytes.NewReader(preamble), request.Body.Body),
		N: s.replayLimits.MaxCompactedBytes + 1,
	}
	if err := s.replayBucket.UploadStream(ctx, objectReference, io.TeeReader(limited, bodyHash)); err != nil {
		return nil, fmt.Errorf("failed to stage compacted replay: %w", err)
	}
	length64 := s.replayLimits.MaxCompactedBytes + 1 - limited.N
	if length64 > s.replayLimits.MaxCompactedBytes {
		return nil, ox.ContentTooLarge{}
	}
	if request.Body.ContentLength >= 0 && request.Body.ContentLength != length64 {
		return nil, ox.BadRequest{}
	}
	bodyDigest := bodyHash.Sum(nil)
	if !bytes.Equal(bodyDigest, expectedDigest) {
		return nil, replayDigestMismatchError{}
	}

	return &stagedCompactedReplay{
		preamble:       preamble,
		preambleDigest: sha256Bytes(preamble),
		bodyDigest:     bodyDigest,
		object:         objectReference,
		length:         int(length64),
		fingerprint: replayRequestFingerprint(
			http.MethodPut,
			request.ReplayID,
			request.IfMatch,
			"",
			strconv.Itoa(preambleLength),
			"",
			"",
			bodyDigest,
		),
	}, nil
}

func replayIdempotentResponse(
	ctx context.Context,
	tx *mapdb.Store,
	replayID string,
	idempotencyKey string,
	fingerprint []byte,
) (*ox.Stream, bool, error) {
	record, err := tx.GetReplayIdempotency(ctx, replayID, idempotencyKey)
	if errors.Is(err, mapdb.ErrNoRows) {
		return nil, false, nil
	} else if err != nil {
		return nil, false, fmt.Errorf("failed to get replay idempotency record: %w", err)
	}
	if !bytes.Equal(record.RequestFingerprint, fingerprint) {
		return nil, false, replayIdempotencyConflictError{}
	}

	var metadata replayResponseMetadata
	if err := json.Unmarshal(record.ResponseMetadata, &metadata); err != nil {
		return nil, false, fmt.Errorf("failed to decode replay idempotency response: %w", err)
	}
	return replayWriteResponse(int(record.ResponseStatus), record.ResponseEtag, metadata), true, nil
}

func createReplayIdempotency(
	ctx context.Context,
	tx *mapdb.Store,
	replayID string,
	idempotencyKey string,
	fingerprint []byte,
	response *ox.Stream,
	metadata replayResponseMetadata,
) error {
	encodedMetadata, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("failed to encode replay idempotency response: %w", err)
	}
	if err := tx.CreateReplayIdempotency(ctx, mapdb.CreateReplayIdempotencyParams{
		ReplayID:           replayID,
		IdempotencyKey:     idempotencyKey,
		RequestFingerprint: fingerprint,
		ResponseStatus:     int16(response.StatusCode),
		ResponseEtag:       response.ETag,
		ResponseMetadata:   encodedMetadata,
	}); err != nil {
		return fmt.Errorf("failed to create replay idempotency record: %w", err)
	}
	return nil
}

func replayWriteResponse(status int, etag string, metadata replayResponseMetadata) *ox.Stream {
	headers := replayStateHeaders(metadata)
	// ox.Stream only writes Content-Length when it is non-zero, and these
	// responses always carry an empty body.
	headers["Content-Length"] = "0"
	return &ox.Stream{
		ContentType: "application/octet-stream",
		Body:        bytes.NewReader(nil),
		ETag:        etag,
		Headers:     headers,
		StatusCode:  status,
	}
}

func replayMetadata(replay mapdb.Replay) replayResponseMetadata {
	metadata := replayResponseMetadata{
		State:          replay.State,
		Representation: replay.Representation,
	}
	if replay.Representation == "segmented" {
		metadata.NextSegmentIndex = &replay.NextSegmentIndex
	}
	return metadata
}

func replayStateHeaders(metadata replayResponseMetadata) map[string]string {
	headers := map[string]string{
		"Replay-State":          metadata.State,
		"Replay-Representation": metadata.Representation,
	}
	if metadata.NextSegmentIndex != nil {
		headers["Replay-Next-Segment-Index"] = strconv.Itoa(*metadata.NextSegmentIndex)
	}
	return headers
}

func validateStoredReplay(replay mapdb.Replay) error {
	if replay.State != "recording" && replay.State != "finished" {
		return fmt.Errorf("stored replay %q has invalid state %q", replay.ID, replay.State)
	}
	switch replay.Representation {
	case "segmented":
		if replay.NextSegmentIndex < 0 {
			return fmt.Errorf("stored replay %q has invalid next segment index", replay.ID)
		}
	case "compacted":
		if replay.State != "finished" || replay.CompactedObject == nil || replay.CompactedLength == nil || replay.CompactedDigest == nil {
			return fmt.Errorf("stored replay %q has invalid compacted state", replay.ID)
		}
	default:
		return fmt.Errorf("stored replay %q has invalid representation %q", replay.ID, replay.Representation)
	}
	if len(replay.CurrentPreambleDigest) != sha256.Size {
		return fmt.Errorf("stored replay %q has invalid preamble digest", replay.ID)
	}
	return nil
}

func validateReplayIDAndKey(replayID, idempotencyKey string) error {
	if replayID == "" || len(replayID) > 512 {
		return ox.BadRequest{}
	}
	if idempotencyKey == "" || len(idempotencyKey) > 512 {
		return ox.BadRequest{}
	}
	return nil
}

// parseReplayWritePrecondition reports whether the commit is creating the
// replay (If-None-Match: *) rather than updating it (If-Match: <etag>).
// Exactly one of the two conditions must be present; there are no
// unconditional replay writes.
func parseReplayWritePrecondition(ifMatch, ifNoneMatch string) (bool, error) {
	switch {
	case ifMatch != "" && ifNoneMatch == "":
		return false, nil
	case ifMatch == "" && ifNoneMatch == "*":
		return true, nil
	default:
		return false, ox.BadRequest{}
	}
}

func parseReplayPreambleLength(value string, maxLength int64) (int, error) {
	length, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || length < 0 {
		return 0, ox.BadRequest{}
	}
	if length > maxLength {
		return 0, ox.ContentTooLarge{}
	}
	return int(length), nil
}

func parseOptionalBool(value string) (bool, error) {
	if value == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, ox.BadRequest{}
	}
	return parsed, nil
}

func parseNonnegativeInt(value string) (int, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 0)
	if err != nil || parsed < 0 {
		return 0, ox.BadRequest{}
	}
	return int(parsed), nil
}

func parseContentDigest(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	const prefix = "sha-256=:"
	if len(value) <= len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) || !strings.HasSuffix(value, ":") {
		return nil, ox.BadRequest{}
	}
	decoded, err := base64.StdEncoding.DecodeString(value[len(prefix) : len(value)-1])
	if err != nil || len(decoded) != sha256.Size {
		return nil, ox.BadRequest{}
	}
	return decoded, nil
}

func mediaTypeMatches(value, expected string) bool {
	if separator := strings.IndexByte(value, ';'); separator >= 0 {
		value = value[:separator]
	}
	return strings.EqualFold(strings.TrimSpace(value), expected)
}

func parseReplayRange(value string, length int64) (start, end int64, partial bool, err error) {
	if value == "" {
		return 0, max(0, length-1), false, nil
	}
	if length <= 0 {
		return 0, 0, false, replayRangeError{length: length}
	}

	value = strings.TrimSpace(value)
	if len(value) < len("bytes=") || !strings.EqualFold(value[:len("bytes=")], "bytes=") {
		return 0, 0, false, replayRangeError{length: length}
	}
	spec := strings.TrimSpace(value[len("bytes="):])
	if spec == "" || strings.Contains(spec, ",") {
		return 0, 0, false, replayRangeError{length: length}
	}
	parts := strings.SplitN(spec, "-", 2)
	if len(parts) != 2 {
		return 0, 0, false, replayRangeError{length: length}
	}

	if parts[0] == "" {
		suffixLength, parseErr := strconv.ParseInt(parts[1], 10, 64)
		if parseErr != nil || suffixLength <= 0 {
			return 0, 0, false, replayRangeError{length: length}
		}
		if suffixLength > length {
			suffixLength = length
		}
		return length - suffixLength, length - 1, true, nil
	}

	start, parseErr := strconv.ParseInt(parts[0], 10, 64)
	if parseErr != nil || start < 0 || start >= length {
		return 0, 0, false, replayRangeError{length: length}
	}
	if parts[1] == "" {
		return start, length - 1, true, nil
	}
	end, parseErr = strconv.ParseInt(parts[1], 10, 64)
	if parseErr != nil || end < start {
		return 0, 0, false, replayRangeError{length: length}
	}
	if end >= length {
		end = length - 1
	}
	return start, end, true, nil
}

func replayRequestFingerprint(method, replayID, ifMatch, ifNoneMatch, preambleLength, segmentIndex, final string, bodyDigest []byte) []byte {
	h := sha256.New()
	writeFingerprintField(h, method)
	writeFingerprintField(h, replayID)
	writeFingerprintField(h, ifMatch)
	writeFingerprintField(h, ifNoneMatch)
	writeFingerprintField(h, preambleLength)
	writeFingerprintField(h, segmentIndex)
	writeFingerprintField(h, final)
	writeFingerprintField(h, base64.StdEncoding.EncodeToString(bodyDigest))
	return h.Sum(nil)
}

func writeFingerprintField(h hash.Hash, value string) {
	_, _ = fmt.Fprintf(h, "%d:", len(value))
	_, _ = io.WriteString(h, value)
}

func replayETag(version int) string {
	return fmt.Sprintf("%q", fmt.Sprintf("r%d", version))
}

func objectETag(digest []byte) string {
	return fmt.Sprintf("%q", "sha256-"+base64.RawURLEncoding.EncodeToString(digest))
}

func contentDigestHeader(digest []byte) string {
	return "sha-256=:" + base64.StdEncoding.EncodeToString(digest) + ":"
}

// replayObjectKey builds a unique staging key for a replay artifact. The
// replay id is hashed because it is client supplied and never validated as a
// uuid, so it must not be able to escape the replays/ prefix. Every call
// returns a new key: staged objects are immutable and are only reachable once
// a commit installs the reference.
func replayObjectKey(replayID, kind string) string {
	idDigest := sha256.Sum256([]byte(replayID))
	return fmt.Sprintf("replays/%s/%s/%s", hex.EncodeToString(idDigest[:]), kind, uuid.NewString())
}

func replayState(final bool) string {
	if final {
		return "finished"
	}
	return "recording"
}

func nextReplaySegmentIndex(current int, segmentIndex *int) int {
	if segmentIndex == nil {
		return current
	}
	return current + 1
}

func sha256Bytes(data []byte) []byte {
	digest := sha256.Sum256(data)
	return digest[:]
}
