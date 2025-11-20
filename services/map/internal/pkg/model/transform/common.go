package transform

import (
	v1 "github.com/hollow-cube/hc-services/services/map/api/v1"
	"github.com/hollow-cube/hc-services/services/map/internal/pkg/model"
)

func Point2API(p *model.Pos) *v1.Point {
	if p == nil {
		return nil
	}
	return &v1.Point{
		X: float32(p.X),
		Y: float32(p.Y),
		Z: float32(p.Z),
	}
}

func Pos2API(p *model.Pos) *v1.Pos {
	if p == nil {
		return nil
	}
	return &v1.Pos{
		X:     float32(p.X),
		Y:     float32(p.Y),
		Z:     float32(p.Z),
		Pitch: float32(p.Pitch),
		Yaw:   float32(p.Yaw),
	}
}
