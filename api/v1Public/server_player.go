package v1Public

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/hollow-cube/api-server/internal/mapdb"
)

// GET /@me/status
func (s *Server) GetPlayerStatus(ctx context.Context, request AuthenticatedRequest) (*PlayerStatus, error) {
	session, err := s.sessionStore.GetPlayerSession(ctx, request.PlayerID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	if session == nil || session.Hidden || errors.Is(err, sql.ErrNoRows) {
		return &PlayerStatus{Online: false, Activity: nil}, nil
	} else {
		var sessionType = GetPlayerActivityTypeFromSession(*session)
		var sessionName *string = nil
		var sessionId *string = nil
		var sessionCode *string = nil

		if sessionType == PlayerActivityPlaying && session.PMapID != nil {
			mapData, err := s.mapStore.GetPublishedMapById(ctx, *session.PMapID)
			if err != nil && !errors.Is(err, mapdb.ErrNoRows) {
				return nil, err
			} else if err == nil && mapData.PublishedMap.OptName != nil && mapData.PublishedMap.PublishedID != nil {
				pid := *mapData.PublishedMap.PublishedID
				code := fmt.Sprintf("%03d-%03d-%03d", pid/1000000, (pid/1000)%1000, pid%1000)

				sessionName = mapData.PublishedMap.OptName
				sessionId = &mapData.PublishedMap.ID
				sessionCode = &code
			}
		}

		return &PlayerStatus{
			Online: true,
			Activity: &PlayerActivity{
				Type: sessionType,
				Name: sessionName,
				Id:   sessionId,
				Code: sessionCode,
			},
		}, nil
	}
}
