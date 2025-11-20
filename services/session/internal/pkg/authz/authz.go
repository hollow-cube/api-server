package authz

import (
	"context"
	"errors"
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
	CheckPlatformPermission(ctx context.Context, userId, cacheKey string, perm PlatformPermission) (State, error)

	HasHypercube(ctx context.Context, userId, cacheKey string) (bool, error)
}

type PlatformPermission string

const (
	PlatformBypassWhitelist PlatformPermission = "bypass_whitelist"
)
