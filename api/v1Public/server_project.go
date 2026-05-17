package v1Public

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"strconv"

	"github.com/hollow-cube/api-server/internal/mapdb"
	"github.com/hollow-cube/api-server/internal/pkg/util"
	"github.com/hollow-cube/api-server/pkg/ox"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

// A project is the editing state of a map. It allows querying and modifying the files that make up
// the map, as well as events for live updates to other editors.
// This API isn't going to be considered stable any time soon, though it is public.
// So may move to a subpath of maps or something.

// TODO: deal with etags and if match/if none match for caching

type (
	Project struct {
		ID   string `json:"id"`
		Name string `json:"name"`

		Files []ProjectFile `json:"files"`
	}
	ProjectFile struct {
		Path        string `json:"path"`
		ContentType string `json:"contentType"`
		Size        int    `json:"size"`
		Hash        string `json:"hash"`
	}
)

type GetProjectRequest struct {
	ProjectID string `path:"projectId"`
}

// GET /projects/{projectId}
func (s *Server) GetProject(ctx context.Context, request GetProjectRequest) (*Project, error) {
	// TODO: obviously need to validate that the person has access to this map

	m, err := s.mapStore.GetMapById(ctx, request.ProjectID)
	if errors.Is(err, mapdb.ErrNoRows) {
		return nil, ox.NotFound{}
	} else if err != nil {
		return nil, err
	}

	proj := Project{ID: m.ID}
	if m.OptName != nil {
		proj.Name = *m.OptName
	} else {
		proj.Name = "Untitled Map"
	}

	files, err := s.mapStore.GetProjectFiles(ctx, m.ID)
	if err != nil {
		return nil, err
	}

	proj.Files = make([]ProjectFile, len(files))
	for i, f := range files {
		proj.Files[i] = ProjectFile{
			Path:        f.Path,
			ContentType: f.ContentType,
			Size:        f.Size,
			Hash:        fmt.Sprintf("%x", f.ContentHash),
		}
	}

	return &proj, nil
}

type GetProjectFileRequest struct {
	ProjectID string `path:"projectId"`
	Path      string `path:"path"`
}

// GET /projects/{projectId}/files/{*path}
// ox:produces application/octet-stream
func (s *Server) GetProjectFile(ctx context.Context, request GetProjectFileRequest) (*ox.Stream, error) {
	path, err := util.NormalizePath(request.Path)
	if err != nil {
		zap.S().Infof("rejecting invalid path: %v", request.Path)
		return nil, ox.BadRequest{}
	}

	// TODO: obviously need to validate that the person has access to this map, and that its a real map.

	file, err := s.mapStore.GetProjectFile(ctx, request.ProjectID, path)
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

type UpdateProjectFileRequest struct {
	ProjectID   string `path:"projectId"`
	Path        string `path:"path"`
	ContentType string `header:"Content-Type,optional"`
	Body        []byte
}

// PUT /projects/{projectId}/files/{*path}
func (s *Server) UpdateProjectFile(ctx context.Context, request UpdateProjectFileRequest) (*ProjectFile, error) {
	path, err := util.NormalizePath(request.Path)
	if err != nil {
		zap.S().Infof("rejecting invalid path: %v", request.Path)
		return nil, ox.BadRequest{}
	}

	// TODO: check content length before doing anything, should reject files too big.

	// TODO: obviously need to validate that the person has access to this map, and that its a real map.

	// TODO: not sure we should blindly trust the content type, but it also may not matter.
	contentType := request.ContentType
	if contentType == "" {
		contentType = "text/plain"
	}

	file, err := s.mapStore.UpsertProjectFile(ctx, mapdb.UpsertProjectFileParams{
		MapID:       request.ProjectID,
		Path:        path,
		Content:     request.Body,
		ContentHash: util.Sha256b(request.Body),
		ContentType: contentType,
	})
	if err != nil {
		return nil, err
	}

	s.publishFileEvent(ctx, request.ProjectID, path)

	return &ProjectFile{
		Path:        file.Path,
		ContentType: file.ContentType,
		Size:        file.Size,
		Hash:        fmt.Sprintf("%x", file.ContentHash),
	}, nil
}

type DeleteProjectFileRequest struct {
	ProjectID string `path:"projectId"`
	Path      string `path:"path"`
}

// DELETE /projects/{projectId}/files/{*path}
func (s *Server) DeleteProjectFile(ctx context.Context, request DeleteProjectFileRequest) error {
	path, err := util.NormalizePath(request.Path)
	if err != nil {
		zap.S().Infof("rejecting invalid path: %v", request.Path)
		return ox.BadRequest{}
	}

	// TODO: obviously need to validate that the person has access to this map, and that its a real map.

	_, err = s.mapStore.DeleteProjectFile(ctx, request.ProjectID, path)
	if errors.Is(err, mapdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return err
	}

	s.publishFileEvent(ctx, request.ProjectID, path)

	return nil
}

// ProjectEvent is a single change notification emitted by StreamProjectEvents.
// Clients receiving an event are expected to refetch the named file.
type ProjectEvent struct {
	Path string `json:"path"`
}

type StreamProjectEventsRequest struct {
	ProjectID   string `path:"projectId"`
	LastEventID string `header:"Last-Event-ID,optional"`
}

// GET /projects/{projectId}/events
func (s *Server) StreamProjectEvents(ctx context.Context, request StreamProjectEventsRequest) (iter.Seq2[ox.Event[ProjectEvent], error], error) {
	var err error

	startSeq := uint64(0)
	startPolicy := jetstream.DeliverNewPolicy
	if request.LastEventID != "" {
		startSeq, err = strconv.ParseUint(request.LastEventID, 10, 64)
		if err != nil {
			return nil, ox.BadRequest{}
		}

		startSeq++ // we want the event after the last one they got, so add 1
		startPolicy = jetstream.DeliverByStartSequencePolicy
	}

	ch := make(chan ox.Event[ProjectEvent], 10)

	cons, err := s.js.SubscribeOrdered(ctx, "MAP_FILES", jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{fmt.Sprintf("map-files.%s", request.ProjectID)},
		DeliverPolicy:  startPolicy,
		OptStartSeq:    startSeq,
	}, func(ctx context.Context, rawMsg jetstream.Msg) error {
		var msg mapFileMessage
		if err := json.Unmarshal(rawMsg.Data(), &msg); err != nil {
			return err
		}

		meta, err := rawMsg.Metadata()
		if err != nil {
			return err
		}

		select {
		case ch <- eventOf(int(meta.Sequence.Stream), msg.Path):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to map files: %w", err)
	}

	if err = cons.Start(); err != nil {
		return nil, fmt.Errorf("failed to start subscription: %w", err)
	}

	return func(yield func(ox.Event[ProjectEvent], error) bool) {
		defer cons.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-ch:
				if !yield(ev, nil) {
					return
				}
			}
		}
	}, nil
}

type mapFileMessage struct {
	MapID string `json:"mapId"`
	Path  string `json:"path"`
}

func (m *mapFileMessage) Subject() string {
	return fmt.Sprintf("map-files.%v", m.MapID)
}

func (s *Server) publishFileEvent(ctx context.Context, mapID, path string) {
	msg := &mapFileMessage{MapID: mapID, Path: path}
	if err := s.js.PublishJSONAsync(ctx, msg); err != nil {
		zap.S().Errorf("failed to publish map file update event: %v", err)
	}
}

func eventOf(id int, path string) ox.Event[ProjectEvent] {
	return ox.Event[ProjectEvent]{
		ID:   strconv.FormatInt(int64(id), 10),
		Data: ProjectEvent{Path: path},
	}
}
