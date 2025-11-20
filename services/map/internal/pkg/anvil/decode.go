package anvil

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"github.com/Tnze/go-mc/nbt"
	"io"
	"time"
)

func Decode[C Chunk](r *RegionFile[C], reader io.Reader) error {
	return NewDecoder[C](reader).Decode(r)
}

type Decoder[C Chunk] struct {
	reader io.Reader
}

func NewDecoder[C Chunk](reader io.Reader) *Decoder[C] {
	return &Decoder[C]{reader}
}

func (d *Decoder[C]) Decode(r *RegionFile[C]) error {
	if r == nil {
		return fmt.Errorf("nil region provided")
	}

	data, err := io.ReadAll(d.reader)
	if err != nil {
		return fmt.Errorf("failed to read region data")
	}

	r.Chunks = make([]*C, 1024)
	r.Timestamps = make([]time.Time, 1024)

	for i := 0; i < 1024; i++ {
		headerStart := i * headerEntrySize

		// Read location from locations table
		locationBytes := append([]byte{0}, data[headerStart:headerStart+3]...)
		location := binary.BigEndian.Uint32(locationBytes)
		sectorCount := uint32(data[headerStart+3])

		if sectorCount == 0 {
			continue // Missing chunk
		}

		// Read timestmp from timestamps table
		timestamp := binary.BigEndian.Uint32(data[headerStart+sectorLen : headerStart+sectorLen+headerEntrySize])
		r.Timestamps[i] = time.Unix(int64(timestamp), 0)

		// Collect the data block and metadata
		paddedData := data[location*sectorLen : (location+sectorCount)*sectorLen]
		compressedLen := binary.BigEndian.Uint32(paddedData[0:headerEntrySize])
		compressedData := paddedData[5 : 5+compressedLen]

		// Ensure compression is zlib, we dont support anything else
		if compressionScheme := int(paddedData[4]); compressionScheme != compressionZlib {
			return fmt.Errorf("unacceptable compression scheme: %d", compressionScheme)
		}

		zlibReader, err := zlib.NewReader(bytes.NewReader(compressedData))
		if err != nil {
			return fmt.Errorf("failed to create zlib chunk data: %w", err)
		}

		var chunk C
		if _, err := nbt.NewDecoder(zlibReader).Decode(&chunk); err != nil {
			return fmt.Errorf("failed to read chunk nbt: %w", err)
		}

		r.Chunks[i] = &chunk
		r.ChunkCount++
	}

	return nil
}
