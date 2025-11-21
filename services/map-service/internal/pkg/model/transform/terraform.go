package transform

import (
	v1 "github.com/hollow-cube/hc-services/services/map-service/api/v1"
	"github.com/hollow-cube/hc-services/services/map-service/internal/pkg/model"
)

func SchematicHeader2API(header *model.SchematicHeader) *v1.SchematicHeader {
	x, y, z := UnpackCoordinate(header.Dimensions)
	return &v1.SchematicHeader{
		Name: header.Name,
		Size: float64(header.Size),
		Dimensions: &v1.TFPoint{
			X: float64(x),
			Y: float64(y),
			Z: float64(z),
		},
	}
}

// PackCoordinate takes a Coordinate and packs it into an int64, preserving the sign.
func PackCoordinate(x, y, z int) int64 {
	return int64(x)<<40 | int64(y)<<20 | int64(z)
}

// UnpackCoordinate takes an int64 and unpacks it into a Coordinate.
func UnpackCoordinate(coord int64) (int, int, int) {
	return int(coord >> 40),
		int(coord >> 20 & 0x0000000000000FFF),
		int(coord & 0x0000000000000FFF)
}
