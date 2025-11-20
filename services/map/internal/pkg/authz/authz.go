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
	Unspecified
)

var ErrNoSuchPermission = errors.New("no such permission")

type Client interface {
	SetMapOwner(ctx context.Context, mapId, userId string) (string, error)
	DeleteMap(ctx context.Context, mapId string) error
	PublishMap(ctx context.Context, mapId string) (string, error)

	CheckPlatformPermission(ctx context.Context, userId, cacheKey string, perm PlatformPermission) (State, error)
	CheckMapPermission(ctx context.Context, mapId, userId, cacheKey string, perm MapPermission) (State, error)

	// Deprecated
	CheckMapRead(ctx context.Context, mapId, userId, cacheKey string) (bool, error)
	// Deprecated
	CheckMapWrite(ctx context.Context, mapId, userId, cacheKey string) (bool, error)
	// Deprecated
	CheckMapAdmin(ctx context.Context, mapId, userId, cacheKey string) (bool, error)
	// Deprecated
	CheckMapGeneric(ctx context.Context, mapId, userId, cacheKey, perm string) (State, error)
}

type PlatformPermission string

const (
	UBuildTools PlatformPermission = "upg_build_tools"
	UMapSlot3   PlatformPermission = "upg_map_slot_3"
	UMapSlot4   PlatformPermission = "upg_map_slot_4"
	UMapSlot5   PlatformPermission = "upg_map_slot_5"
	UMapSize2   PlatformPermission = "upg_map_size_2"
	UMapSize3   PlatformPermission = "upg_map_size_3"
	UMapSize4   PlatformPermission = "upg_map_size_4"
)

var PlatformPermissionValidationMap = map[PlatformPermission]bool{
	UBuildTools: true,
	UMapSlot3:   true,
	UMapSlot4:   true,
	UMapSlot5:   true,
	UMapSize2:   true,
	UMapSize3:   true,
	UMapSize4:   true,
}

type MapPermission string

const (
	MapEditSettings         MapPermission = "edit_settings"
	MapDelete               MapPermission = "delete"
	MapVerify               MapPermission = "verify"
	MapPublish              MapPermission = "publish"
	MapCopy                 MapPermission = "copy"
	MapTransfer             MapPermission = "transfer"
	MapManageTrusted        MapPermission = "manage_trusted"
	MapEditGameplaySettings MapPermission = "edit_gameplay_settings"
	MapEditWorld            MapPermission = "edit_world"
	MapInviteTemp           MapPermission = "invite_temp"
	MapKickTemp             MapPermission = "kick_temp"
	MapPlay                 MapPermission = "play"
)

var MapPermissionValidationMap = map[MapPermission]bool{
	MapEditSettings:         true,
	MapDelete:               true,
	MapVerify:               true,
	MapPublish:              true,
	MapCopy:                 true,
	MapTransfer:             true,
	MapManageTrusted:        true,
	MapEditGameplaySettings: true,
	MapEditWorld:            true,
	MapInviteTemp:           true,
	MapKickTemp:             true,
	MapPlay:                 true,
}
