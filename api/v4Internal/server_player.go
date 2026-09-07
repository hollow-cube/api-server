package v4Internal

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hollow-cube/api-server/internal/playerdb"
	"github.com/hollow-cube/api-server/pkg/hog"
)

type CreatePlayerDataRequest struct {
	ID       string      `json:"id"`
	IP       string      `json:"ip"`
	Skin     *PlayerSkin `json:"skin"`
	Username string      `json:"username"`
}

// POST /players
func (s *Server) CreatePlayerData(ctx context.Context, body CreatePlayerDataRequest) (*PlayerData, error) {
	var skin *playerdb.PlayerSkin
	if body.Skin != nil {
		skin = &playerdb.PlayerSkin{
			Texture:   body.Skin.Texture,
			Signature: body.Skin.Signature,
		}
	}

	pd, err := s.playerStore.CreatePlayerData(ctx, body.ID, body.Username, skin)
	if err != nil {
		return nil, fmt.Errorf("failed to create player data: %w", err)
	}

	err = s.playerStore.AddPlayerIP(ctx, pd.ID, body.IP)
	if err != nil {
		return nil, fmt.Errorf("failed to add player ip: %w", err)
	}

	hog.Enqueue(hog.Capture{
		Event: "new_player",
		Properties: hog.NewProperties().
			Set("player_id", pd.ID),
	})

	return s.hydratePlayerData(ctx, pd)
}

// GET /players/{playerId}

// PATCH /players/{playerId}
// GET /players/{playerId}/display-name

// GET /players/{playerId}/hypercube

// GET /players/{playerId}/alts

// POST /players/search
func (s *Server) hydratePlayerData(ctx context.Context, pd playerdb.PlayerData) (*PlayerData, error) {
	// Can test empty code to see if TOTP is disabled
	//_, err := s.testTotpCode(ctx, pd.ID, "", false)
	//totpEnabled := true
	//if errors.Is(err, errNotConfigured) {
	//	totpEnabled = false
	//} else if err != nil {
	//	return nil, fmt.Errorf("failed to check totp status: %w", err)
	//}
	totpEnabled := false

	// always return empty map not null if no settings have been changed
	// TODO: should just make the column notnull and then this isnt necessary
	var settings map[string]interface{}
	if pd.Settings != nil {
		settings = pd.Settings
	} else {
		settings = make(map[string]interface{})
	}

	var skin *PlayerSkin
	if pd.Skin != nil {
		skin = &PlayerSkin{
			Texture:   pd.Skin.Texture,
			Signature: pd.Skin.Signature,
		}
	}

	return &PlayerData{
		Id:          pd.ID,
		Username:    pd.Username,
		DisplayName: s.computeDisplayName(pd),
		Skin:        skin,
		FirstJoin:   pd.FirstJoin,
		LastOnline:  pd.LastOnline,
		Playtime:    pd.Playtime,
		Settings:    settings,
		Experience:  0,
		BetaEnabled: *pd.BetaEnabled, //todo should make this column notnull

		Coins:          0,
		Cubits:         pd.Cubits,
		HypercubeUntil: pd.HypercubeEnd,

		TotpEnabled: totpEnabled,

		Permissions:    strconv.FormatUint(uint64(pd.Flags()), 10),
		MapSlots:       pd.TotalMapSlots(),
		MapBuilders:    pd.TotalBuilderSlots(),
		TempMaxMapSize: int(pd.MaxMapSize),
	}, nil
}

var orgMapNames = map[string]DisplayName{
	"b571aed9-19f4-4032-9c06-75a4b7cf6c00": {
		DisplayNamePart{Type: "username", Text: "Hollow Cube", Color: "#3895ff"},
	},
}

func (s *Server) computeDisplayName(pd playerdb.PlayerData) DisplayName {
	var parts DisplayName

	role := pd.EffectiveRole()
	if role.Badge() != "" {
		parts = append(parts, DisplayNamePart{
			Type: "badge",
			Text: role.Badge(),
		})
	}

	parts = append(parts, DisplayNamePart{
		Type:  "username",
		Text:  pd.Username,
		Color: role.Color(),
	})

	return parts
}
