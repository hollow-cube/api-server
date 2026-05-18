package v4Internal

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/util"
	"github.com/hollow-cube/api-server/pkg/ox"
)

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
func (s *Server) ListMapFiles(ctx context.Context, request MapRequest) (*FileList, error) {
	files, err := s.mapStore.GetMapFiles(ctx, request.MapID)
	if err != nil {
		return nil, err
	}

	results := make([]FileHeader, len(files))
	for i, f := range files {
		results[i] = FileHeader{
			Path:        f.Path,
			ContentType: f.ContentType,
			Size:        f.Size,
			Hash:        fmt.Sprintf("%x", f.ContentHash),
		}
	}

	return &FileList{Results: results}, nil
}

type GetMapFileRequest struct {
	MapID string `path:"mapId"`
	Path  string `path:"path"`
}

// GET /maps/{mapId}/files/{*path}
// ox:produces application/octet-stream
func (s *Server) GetMapFile(ctx context.Context, request GetMapFileRequest) (*ox.Stream, error) {
	path, err := util.NormalizePath(request.Path)
	if err != nil {
		return nil, ox.BadRequest{}
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
