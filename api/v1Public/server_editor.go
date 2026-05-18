package v1Public

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strconv"

	"github.com/hollow-cube/api-server/pkg/ox"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

type (
	EditorBootstrap struct {
		Map   EditorMapInfo `json:"map"`
		Files []FileHeader  `json:"files"`
	}
	EditorMapInfo struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Owner string `json:"owner"`
	}
)

// GET /maps/{mapId}/editor/bootstrap
func (s *Server) GetEditorBootstrap(ctx context.Context, request MapRequest) (*EditorBootstrap, error) {
	m, err := s.mapForAuthPlayer(ctx, request.MapID)
	if err != nil {
		return nil, err
	}

	bootstrap := EditorBootstrap{}
	bootstrap.Map.ID = m.ID
	if m.OptName != nil {
		bootstrap.Map.Name = *m.OptName
	} else {
		bootstrap.Map.Name = "Untitled Map"
	}
	bootstrap.Map.Owner = m.Owner

	files, err := s.mapStore.GetMapFiles(ctx, m.ID)
	if err != nil {
		return nil, err
	}

	bootstrap.Files = make([]FileHeader, len(files))
	for i, f := range files {
		bootstrap.Files[i] = FileHeader{
			Path:        f.Path,
			ContentType: f.ContentType,
			Size:        f.Size,
			Hash:        fmt.Sprintf("%x", f.ContentHash),
		}
	}

	return &bootstrap, nil
}

type EditorEvent struct {
	Path string `json:"path"`
}

type StreamEditorEventsRequest struct {
	MapID       string `path:"mapId"`
	LastEventID string `header:"Last-Event-ID,optional"` // Allows resuming on reconnect
}

// GET /maps/{mapId}/editor/events
func (s *Server) StreamEditorEvents(ctx context.Context, request StreamEditorEventsRequest) (iter.Seq2[ox.Event[EditorEvent], error], error) {
	// Ensure player has access
	_, err := s.mapForAuthPlayer(ctx, request.MapID)
	if err != nil {
		return nil, err
	}

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

	ch := make(chan ox.Event[EditorEvent], 10)

	cons, err := s.js.SubscribeOrdered(ctx, "MAP_FILES", jetstream.OrderedConsumerConfig{
		FilterSubjects: []string{fmt.Sprintf("map-files.%s", request.MapID)},
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

	return func(yield func(ox.Event[EditorEvent], error) bool) {
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

func eventOf(id int, path string) ox.Event[EditorEvent] {
	return ox.Event[EditorEvent]{
		ID:   strconv.FormatInt(int64(id), 10),
		Data: EditorEvent{Path: path},
	}
}
