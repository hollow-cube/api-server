package v1Public

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/util"
	"github.com/hollow-cube/api-server/pkg/ox"
	"go.uber.org/zap"
)

const maxFileSize = 1 * 1024 * 1024 // 1 MB

type (
	FileList struct {
		Results []FileHeader `json:"results"`
	}
	FileHeader struct {
		Path        string `json:"path"`
		ContentType string `json:"contentType"`
		Size        int    `json:"size"`
		Hash        string `json:"hash"`
	}
)

// GET /maps/{mapId}/files
func (s *Server) GetMapFiles(ctx context.Context, request MapRequest) (*FileList, error) {
	m, err := s.mapForAuthPlayer(ctx, request.MapID)
	if err != nil {
		return nil, err
	}

	files, err := s.mapStore.GetMapFiles(ctx, m.ID)
	if err != nil {
		return nil, err
	}

	result := make([]FileHeader, len(files))
	for i, f := range files {
		result[i] = FileHeader{
			Path:        f.Path,
			ContentType: f.ContentType,
			Size:        f.Size,
			Hash:        fmt.Sprintf("%x", f.ContentHash),
		}
	}

	return &FileList{Results: result}, nil
}

type GetMapFileRequest struct {
	MapID       string `path:"mapId"`
	Path        string `path:"path"`
	IfNoneMatch string `header:"If-None-Match,optional"`
}

// GET /maps/{mapId}/files/{*path}
// ox:produces application/octet-stream
func (s *Server) GetMapFile(ctx context.Context, request GetMapFileRequest) (*ox.Stream, error) {
	path, err := util.NormalizePath(request.Path)
	if err != nil {
		zap.S().Infof("rejecting invalid path: %v", request.Path)
		return nil, ox.BadRequest{}
	}

	// Ensure the map exists and the player has access
	if _, err = s.mapForAuthPlayer(ctx, request.MapID); err != nil {
		return nil, err
	}

	file, err := s.mapStore.GetMapFile(ctx, request.MapID, path)
	if errors.Is(err, mapdb.ErrNoRows) {
		return nil, ox.NotFound{}
	} else if err != nil {
		return nil, err
	}

	etag := etagFor(file.ContentHash)
	if request.IfNoneMatch != "" && etagListMatches(request.IfNoneMatch, etag) {
		return nil, ox.NotModified{}
	}

	return &ox.Stream{
		ContentType:   file.ContentType,
		Body:          bytes.NewReader(file.Content),
		ContentLength: int64(file.Size),
		ETag:          etag,
	}, nil
}

type UpdateMapFileRequest struct {
	MapID       string `path:"mapId"`
	Path        string `path:"path"`
	ContentType string `header:"Content-Type,optional"`
	IfMatch     string `header:"If-Match,optional"`
	IfNoneMatch string `header:"If-None-Match,optional"`
	Body        io.Reader
}

// PUT /maps/{mapId}/files/{*path}
func (s *Server) UpdateMapFile(ctx context.Context, request UpdateMapFileRequest) (*FileHeader, error) {
	path, err := util.NormalizePath(request.Path)
	if err != nil {
		zap.S().Infof("rejecting invalid path: %v", request.Path)
		return nil, ox.BadRequest{}
	}

	// Ensure the map exists and the player has access
	if _, err = s.mapForAuthPlayer(ctx, request.MapID); err != nil {
		return nil, err
	}

	if request.IfMatch != "" || request.IfNoneMatch != "" {
		exists, hash, err := s.currentFileETagState(ctx, request.MapID, path)
		if err != nil {
			return nil, err
		}
		if err := checkWritePreconditions(exists, hash, request.IfMatch, request.IfNoneMatch); err != nil {
			return nil, err
		}
	}

	// Bound the read so an oversized (or unbounded) body cannot exhaust memory.
	// Reading maxFileSize+1 lets us distinguish "exactly at the limit" from
	// "over the limit" without buffering the whole payload.
	content, err := io.ReadAll(io.LimitReader(request.Body, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxFileSize {
		zap.S().Infof("rejecting file that is too large: > %d bytes", maxFileSize)
		return nil, ox.BadRequest{}
	}

	// TODO: not sure we should blindly trust the content type, but it also may not matter.
	contentType := request.ContentType
	if contentType == "" {
		contentType = "text/plain"
	}

	file, err := s.mapStore.UpsertMapFile(ctx, mapdb.UpsertMapFileParams{
		MapID:       request.MapID,
		Path:        path,
		Content:     content,
		ContentHash: util.Sha256b(content),
		ContentType: contentType,
	})
	if err != nil {
		return nil, err
	}

	s.publishFileEvent(ctx, request.MapID, path)

	return &FileHeader{
		Path:        file.Path,
		ContentType: file.ContentType,
		Size:        file.Size,
		Hash:        fmt.Sprintf("%x", file.ContentHash),
	}, nil
}

type DeleteMapFileRequest struct {
	MapID       string `path:"mapId"`
	Path        string `path:"path"`
	IfMatch     string `header:"If-Match,optional"`
	IfNoneMatch string `header:"If-None-Match,optional"`
}

// DELETE /maps/{mapId}/files/{*path}
func (s *Server) DeleteMapFile(ctx context.Context, request DeleteMapFileRequest) error {
	path, err := util.NormalizePath(request.Path)
	if err != nil {
		zap.S().Infof("rejecting invalid path: %v", request.Path)
		return ox.BadRequest{}
	}

	if _, err = s.mapForAuthPlayer(ctx, request.MapID); err != nil {
		return err
	}

	if request.IfMatch != "" || request.IfNoneMatch != "" {
		exists, hash, err := s.currentFileETagState(ctx, request.MapID, path)
		if err != nil {
			return err
		}
		if err := checkWritePreconditions(exists, hash, request.IfMatch, request.IfNoneMatch); err != nil {
			return err
		}
	}

	_, err = s.mapStore.DeleteMapFile(ctx, request.MapID, path)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return err
	}

	s.publishFileEvent(ctx, request.MapID, path)

	return nil
}

func etagFor(hash []byte) string {
	// quoted, per RFC-7232
	return fmt.Sprintf("%q", fmt.Sprintf("%x", hash))
}

func etagListMatches(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "*" {
		return true
	}
	for _, tag := range strings.Split(header, ",") {
		tag = strings.TrimPrefix(strings.TrimSpace(tag), "W/")
		if tag == etag {
			return true
		}
	}
	return false
}

func checkWritePreconditions(exists bool, hash []byte, ifMatch, ifNoneMatch string) error {
	if ifMatch != "" && (!exists || !etagListMatches(ifMatch, etagFor(hash))) {
		return ox.PreconditionFailed{}
	}
	if ifNoneMatch != "" && exists && etagListMatches(ifNoneMatch, etagFor(hash)) {
		return ox.PreconditionFailed{}
	}
	return nil
}

func (s *Server) currentFileETagState(ctx context.Context, mapID, path string) (exists bool, hash []byte, err error) {
	cur, err := s.mapStore.GetMapFile(ctx, mapID, path)
	if errors.Is(err, mapdb.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	return true, cur.ContentHash, nil
}
