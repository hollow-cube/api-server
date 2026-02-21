package intnl

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
)

func (s *server) hydratePlayerData(ctx context.Context, pd db.PlayerData) (*PlayerData, error) {
	// Can test empty code to see if TOTP is disabled
	_, err := s.testTotpCode(ctx, pd.ID, "", false)
	totpEnabled := true
	if errors.Is(err, errNotConfigured) {
		totpEnabled = false
	} else if err != nil {
		return nil, fmt.Errorf("failed to check totp status: %w", err)
	}

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
		Id:            pd.ID,
		Username:      pd.Username,
		DisplayNameV2: computeDisplayNameV2(pd),
		Skin:          skin,
		FirstJoin:     pd.FirstJoin,
		LastOnline:    pd.LastOnline,
		Playtime:      pd.Playtime,
		Settings:      settings,
		Experience:    0,
		BetaEnabled:   *pd.BetaEnabled, //todo should make this column notnull

		Coins:          0,
		Cubits:         pd.Cubits,
		HypercubeUntil: pd.HypercubeEnd,

		TotpEnabled: totpEnabled,

		Permissions: strconv.FormatUint(uint64(pd.Flags()), 10),
	}, nil
}
