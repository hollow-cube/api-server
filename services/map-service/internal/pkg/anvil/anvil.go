package anvil

import (
	"time"
)

const (
	sectorLen       = 4 * 1024
	headerEntrySize = 4
	compressionZlib = 2
)

type Chunk interface {
	ChunkMarker()
}

type RegionFile[C Chunk] struct {
	Timestamps []time.Time
	Chunks     []*C
	// ChunkCount is the number of chunks which are present in the region.
	// This is never serialized in the region file.
	ChunkCount int
}
