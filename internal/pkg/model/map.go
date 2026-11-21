package model

import "fmt"

type MapSize int

const (
	MapSizeUnlimitedOldNotReal = iota - 1
	MapSizeNormal
	MapSizeLarge
	MapSizeMassive
	MapSizeColossal
	MapSizeUnlimited
	MapSizeTall2k
	MapSizeTall4k
)

type MapUpdateAction int

const (
	MapUpdate_Create MapUpdateAction = iota
	MapUpdate_Delete
	MapUpdate_Drain
)

func (a MapUpdateAction) String() string {
	switch a {
	case MapUpdate_Create:
		return "create"
	case MapUpdate_Delete:
		return "delete"
	case MapUpdate_Drain:
		return "drain"
	default:
		return "unknown"
	}
}

type MapUpdateMessage struct {
	Action MapUpdateAction `json:"action"`
	ID     string          `json:"id"`

	DrainReason *string `json:"drainReason"`
}

func (m MapUpdateMessage) Subject() string {
	return fmt.Sprintf("map.%s", m.Action.String())
}
