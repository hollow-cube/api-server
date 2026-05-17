package v1Public

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/util"
	"github.com/hollow-cube/api-server/pkg/ox"
	"go.uber.org/zap"
)

// TODO: deal with etags and if match/if none match for caching

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
	MapID string `path:"mapID"`
	Path  string `path:"path"`
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

	return &ox.Stream{
		ContentType:   file.ContentType,
		Body:          bytes.NewReader(file.Content),
		ContentLength: int64(file.Size),
	}, nil
}

type UpdateMapFileRequest struct {
	MapID       string `path:"mapId"`
	Path        string `path:"path"`
	ContentType string `header:"Content-Type,optional"`
	Body        []byte
}

// PUT /maps/{mapId}/files/{*path}
func (s *Server) UpdateMapFile(ctx context.Context, request UpdateMapFileRequest) (*FileHeader, error) {
	path, err := util.NormalizePath(request.Path)
	if err != nil {
		zap.S().Infof("rejecting invalid path: %v", request.Path)
		return nil, ox.BadRequest{}
	}

	if len(request.Body) > maxFileSize {
		zap.S().Infof("rejecting file that is too large: %d bytes", len(request.Body))
		return nil, ox.BadRequest{}
	}

	// Ensure the map exists and the player has access
	if _, err = s.mapForAuthPlayer(ctx, request.MapID); err != nil {
		return nil, err
	}

	// TODO: not sure we should blindly trust the content type, but it also may not matter.
	contentType := request.ContentType
	if contentType == "" {
		contentType = "text/plain"
	}

	file, err := s.mapStore.UpsertMapFile(ctx, mapdb.UpsertMapFileParams{
		MapID:       request.MapID,
		Path:        path,
		Content:     request.Body,
		ContentHash: util.Sha256b(request.Body),
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
	MapID string `path:"mapId"`
	Path  string `path:"path"`
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

	_, err = s.mapStore.DeleteMapFile(ctx, request.MapID, path)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return err
	}

	s.publishFileEvent(ctx, request.MapID, path)

	return nil
}
