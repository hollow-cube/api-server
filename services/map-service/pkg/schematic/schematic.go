package schematic

import (
	"compress/gzip"
	"fmt"
	"io"

	"github.com/Tnze/go-mc/nbt"
)

type Schematic struct {
	Width      int
	Height     int
	Length     int
	Metadata   *Metadata
	Palette    map[string]int
	PaletteMax int
	BlockData  []byte
}

type Metadata struct {
	WEOffsetX int
	WEOffsetY int
	WEOffsetZ int
}

func Read(reader io.Reader, compressed bool) (*Schematic, error) {
	var err error

	if compressed {
		reader, err = gzip.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress: %w", err)
		}
	}

	var schem Schematic
	if _, err = nbt.NewDecoder(reader).Decode(&schem); err != nil {
		return nil, fmt.Errorf("failed to unmarshal nbt: %w", err)
	}

	return &schem, nil
}
