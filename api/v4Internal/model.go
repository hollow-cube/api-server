package v4Internal

import "github.com/hollow-cube/api-server/internal/mapdb"

type PlayerPaginatedRequest struct {
	PlayerID string `path:"playerId"`
	Page     int    `query:"page"`
	PageSize int    `query:"pageSize"`
}

type Pos struct {
	Point
	Pitch float64 `json:"pitch"`
	Yaw   float64 `json:"yaw"`
}

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

func hydratePos(pos mapdb.Pos) Pos {
	return Pos{
		Point: Point{X: pos.X, Y: pos.Y, Z: pos.Z},
		Yaw:   pos.Yaw,
		Pitch: pos.Pitch,
	}
}

func dbPos(pos Pos) mapdb.Pos {
	return mapdb.Pos{
		X:     pos.X,
		Y:     pos.Y,
		Z:     pos.Z,
		Yaw:   pos.Yaw,
		Pitch: pos.Pitch,
	}
}
