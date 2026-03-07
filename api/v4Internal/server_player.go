package v4Internal

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/hollow-cube/api-server/internal/pkg/util"
	"github.com/hollow-cube/api-server/internal/playerdb"
	"github.com/hollow-cube/api-server/pkg/hog"
	"github.com/hollow-cube/api-server/pkg/ox"
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

type PlayerRequest struct {
	PlayerId string `path:"playerId"`
}

// GET /players/{playerId}
func (s *Server) GetPlayerData(ctx context.Context, request PlayerRequest) (*PlayerData, error) {
	pd, err := s.playerStore.GetPlayerData(ctx, util.RemapUUID(request.PlayerId))
	if errors.Is(err, playerdb.ErrNoRows) {
		return nil, ox.NotFound{}
	} else if err != nil {
		return nil, err
	}

	return s.hydratePlayerData(ctx, pd)
}

type (
	UpdatePlayerDataRequest struct {
		PlayerId string `path:"playerId"`
		UpdatePlayerDataRequestBody
	}
	UpdatePlayerDataRequestBody struct {
		BetaEnabled *bool `json:"betaEnabled"`

		// IpHistory New IP addresses to add to the history, will be merged.
		IpHistory       *[]string       `json:"ipHistory"`
		LastOnline      *time.Time      `json:"lastOnline"`
		PlaytimeInc     *int            `json:"playtimeInc"`
		SettingsUpdates *PlayerSettings `json:"settingsUpdates"`
		Skin            *PlayerSkin     `json:"skin"`
		Username        *string         `json:"username"`
	}
)

// PATCH /players/{playerId}
func (s *Server) UpdatePlayerData(ctx context.Context, request UpdatePlayerDataRequest) error {
	p, err := s.playerStore.GetPlayerData(ctx, request.PlayerId)
	if errors.Is(err, playerdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to get player data: %w", err)
	}

	var changed bool
	dbUpdates := playerdb.UpdatePlayerDataParams{ID: p.ID, Skin: p.Skin}
	if request.Username != nil && *request.Username != p.Username {
		dbUpdates.Username = request.Username
		changed = true
	}
	if request.LastOnline != nil {
		dbUpdates.LastOnline = request.LastOnline
		changed = true
	}
	if request.PlaytimeInc != nil {
		dbUpdates.Playtime = new(p.Playtime + *request.PlaytimeInc)
		changed = true
	}
	if request.BetaEnabled != nil {
		dbUpdates.BetaEnabled = request.BetaEnabled
		changed = true
	}
	if request.SettingsUpdates != nil {
		for key, value := range *request.SettingsUpdates {
			p.Settings[key] = value
			dbUpdates.Settings = p.Settings
			changed = true
		}
	}
	if request.Skin != nil {
		dbUpdates.Skin = &playerdb.PlayerSkin{
			Texture:   request.Skin.Texture,
			Signature: request.Skin.Signature,
		}
		changed = true
	}

	err = playerdb.TxNoReturn(ctx, s.playerStore, func(ctx context.Context, tx *playerdb.Store) error {
		if request.IpHistory != nil && len(*request.IpHistory) > 0 {
			for _, ip := range *request.IpHistory {
				if ip == "" {
					continue
				}

				if err = tx.AddPlayerIP(ctx, p.ID, ip); err != nil {
					return fmt.Errorf("failed to record player ip: %w", err)
				}
			}
			changed = true
		}

		if !changed {
			return nil
		}

		err = tx.UpdatePlayerData(ctx, dbUpdates)
		if err != nil {
			return fmt.Errorf("failed to update player data: %w", err)
		}

		return nil
	})
	if errors.Is(err, playerdb.ErrNoRows) {
		return ox.NotFound{}
	} else if err != nil {
		return fmt.Errorf("failed to apply transaction: %w", err)
	}

	return nil
}

// GET /players/{playerId}/display-name
func (s *Server) GetPlayerDisplayName(ctx context.Context, request PlayerRequest) (DisplayName, error) {
	if orgName, ok := orgMapNames[request.PlayerId]; ok {
		return orgName, nil
	}

	pd, err := s.playerStore.GetPlayerData(ctx, request.PlayerId)
	if errors.Is(err, playerdb.ErrNoRows) {
		return nil, ox.NotFound{}
	} else if err != nil {
		return nil, fmt.Errorf("failed to get player data: %w", err)
	}

	return s.computeDisplayName(pd), nil
}

type (
	GetPlayerAltsResponse struct {
		Results []PlayerAltAccount `json:"results"`
	}
	PlayerAltAccount struct {
		Id       string `json:"id"`
		Username string `json:"username"`
	}
)

// GET /players/{playerId}/alts
func (s *Server) GetPlayerAlts(ctx context.Context, request PlayerRequest) (*GetPlayerAltsResponse, error) {
	playerIPs, err := s.playerStore.GetPlayerIPHistory(ctx, request.PlayerId)
	if err != nil {
		return nil, err
	}

	sharedPlayers, err := s.playerStore.GetPlayersByIPs(ctx, playerIPs)
	if err != nil {
		return nil, err
	}

	results := make([]PlayerAltAccount, 0, 10)
	for _, row := range sharedPlayers {
		if row.ID == request.PlayerId {
			continue
		}

		results = append(results, PlayerAltAccount{
			Id:       row.ID,
			Username: row.Username,
		})
	}

	return &GetPlayerAltsResponse{Results: results}, nil
}

type (
	SearchPlayersRequest struct {
		Query   string   `json:"query"`
		Exclude []string `json:"exclude"` // Exclude the players in this list

		Limit int `json:"limit"`
	}
	SearchPlayersResponse struct {
		Results []PlayerDataStub `json:"results"`
	}
	PlayerDataStub struct {
		ID          string      `json:"id"`
		DisplayName DisplayName `json:"displayName"`
	}
)

// POST /players/search
func (s *Server) SearchPlayers(ctx context.Context, body SearchPlayersRequest) (*SearchPlayersResponse, error) {
	if body.Query == "" {
		return &SearchPlayersResponse{Results: []PlayerDataStub{}}, nil
	}

	limit := int32(body.Limit)
	if limit == 0 {
		limit = 25
	}
	limit = min(max(limit, 1), 100)

	pds, err := s.playerStore.SearchPlayersFuzzy(ctx, body.Exclude, body.Query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to search players: %w", err)
	}

	results := make([]PlayerDataStub, len(pds))
	for i, entry := range pds {
		results[i] = PlayerDataStub{
			ID:          entry.ID,
			DisplayName: s.computeDisplayName(entry),
		}
	}
	return &SearchPlayersResponse{Results: results}, nil
}

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
