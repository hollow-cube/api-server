package intnl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/hollow-cube/hc-services/services/player-service/internal/db"
	"github.com/hollow-cube/hc-services/services/player-service/internal/pkg/authz"
)

// getPlayerHypercubeTime returns the end time of the player's current hypercube subscription
// or nil if they do not currently have hypercube.
func (s *server) getPlayerHypercubeTime(ctx context.Context, playerId string) (*time.Time, error) {
	hasHypercube, err := s.authzClient.HasHypercube(ctx, playerId, authz.NoKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check hypercube status: %w", err)
	}
	if !hasHypercube {
		return nil, nil
	}

	// Try to get the time, though there may not be an entry because some people get it implicitly
	// from other relationships. In that case we should simply grant it for a year from now.
	hcStartTime, hcTerm, err := s.authzClient.GetHypercubeStats(ctx, playerId, authz.NoKey)
	if errors.Is(err, authz.ErrNotFound) {
		// Implicit grant
		temp := time.Now().Add(365 * 24 * time.Hour)
		return &temp, nil
	} else if err == nil {
		temp := hcStartTime.Add(hcTerm)
		if time.Now().After(temp) {
			// This is also an implicit grant case, kinda gross but oh well
			temp = time.Now().Add(365 * 24 * time.Hour)
		}
		return &temp, nil
	}

	return nil, fmt.Errorf("failed to check hypercube time: %w", err)
}

func (s *server) hydratePlayerData(ctx context.Context, pd db.PlayerData) (*PlayerData, error) {
	hypercubeTime, err := s.getPlayerHypercubeTime(ctx, pd.ID)
	if err != nil {
		return nil, err
	}

	var ok bool
	var displayName2 DisplayNameV2
	// do not reenable this for now. its because getting hypercube doesnt currently wipe it.
	if displayName2, ok = s.nameCache2.Get(pd.ID); !ok || true {
		displayName2, err = s.computeDisplayNameV2(ctx, pd.ID, pd.Username)
		if err != nil {
			return nil, fmt.Errorf("failed to compute display name 2: %w", err)
		}
	}

	// Can test empty code to see if TOTP is disabled
	_, err = s.testTotpCode(ctx, pd.ID, "", false)
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
		DisplayNameV2: displayName2,
		Skin:          skin,
		FirstJoin:     pd.FirstJoin,
		LastOnline:    pd.LastOnline,
		Playtime:      pd.Playtime,
		Settings:      settings,
		Experience:    0,
		BetaEnabled:   *pd.BetaEnabled, //todo should make this column notnull

		Coins:          0,
		Cubits:         pd.Cubits,
		HypercubeUntil: hypercubeTime,

		TotpEnabled: totpEnabled,
	}, nil
}
