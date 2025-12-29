package authz

//go:generate mockgen -source=authz.go -destination=mock_authz/authz.gen.go

import (
	"context"
	"errors"
	"time"
)

const NoKey = ""

type State int

const (
	Deny State = iota
	Allow
	Conditional
	Unspecified
)

var (
	ErrNoSuchPermission = errors.New("no such permission")
	ErrNotFound         = errors.New("not found")
)

type Client interface {
	MultiCheckPlatformPermission(ctx context.Context, userIds []string, cacheKey string, perm PlatformPermission) (map[string]State, error)
	CheckPlatformPermission(ctx context.Context, userId, cacheKey string, perm PlatformPermission) (State, error)

	// Hypercube

	HasHypercube(ctx context.Context, userId, cacheKey string) (bool, error)
	GetHypercubeStats(ctx context.Context, playerId string, cacheKey string) (time.Time, time.Duration, error)
	AppendHypercube(ctx context.Context, playerId string, addedTerm time.Duration, cacheKey string) error

	// Upgrades
	UnlockUpgrade(ctx context.Context, playerId, upgradeId, cacheKey string) error
	RemoveUpgrade(ctx context.Context, playerId, upgradeId, cacheKey string) error
}

type PlatformPermission string

const (
	// PlatformBanPlayer is regularly used throughout the codebase to check if a player has a level of staff permissions,
	// outside just being able to ban a player.
	PlatformBanPlayer PlatformPermission = "ban_player"

	PlatformPrefixHollowcube PlatformPermission = "prefix_hollowcube"
	PlatformPrefixAdmin      PlatformPermission = "prefix_admin"
	PlatformPrefixDev        PlatformPermission = "prefix_dev"
	PlatformPrefixMod        PlatformPermission = "prefix_mod"
	PlatformPrefixCrt        PlatformPermission = "prefix_crt"
)

var PlatformPermissionValidationMap = map[PlatformPermission]bool{
	PlatformBanPlayer: true,

	PlatformPrefixHollowcube: true,
	PlatformPrefixAdmin:      true,
	PlatformPrefixDev:        true,
	PlatformPrefixMod:        true,
	PlatformPrefixCrt:        true,
}
