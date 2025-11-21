package anvil

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"github.com/Tnze/go-mc/nbt"
	"io"
)

func Encode[C Chunk](r *RegionFile[C], writer io.Writer) error {
	return NewEncoder[C](writer).Encode(r)
}

type Encoder[C Chunk] struct {
	writer io.Writer
}

func NewEncoder[C Chunk](writer io.Writer) *Encoder[C] {
	return &Encoder[C]{writer}
}

func (e *Encoder[C]) Encode(r *RegionFile[C]) error {
	if r == nil {
		return fmt.Errorf("nil region provided")
	}

	var buffer bytes.Buffer
	buffer.Write(make([]byte, 2*sectorLen)) // Ensure cap for tables

	for i := 0; i < 1024; i++ {
		if r.Chunks[i] == nil {
			continue // missing chunk
		}

		// Write compressed chunk data
		chunkStart := buffer.Len()
		buffer.Write(make([]byte, 5)) // Header

		chunkData, err := nbt.Marshal(r.Chunks[i])
		if err != nil {
			return fmt.Errorf("failed to marshal chunk data: %w", err)
		}

		zlibWriter := zlib.NewWriter(&buffer)
		if _, err := zlibWriter.Write(chunkData); err != nil {
			return fmt.Errorf("failed to compress chunk data: %w", err)
		}
		zlibWriter.Close()

		chunkEnd := buffer.Len()
		buffer.Write(make([]byte, sectorLen-(chunkEnd%sectorLen))) // Padding

		// Fill chunk header
		chunkLen := uint32(chunkEnd - chunkStart - 5)
		binary.BigEndian.PutUint32(buffer.Bytes()[chunkStart:], chunkLen)
		buffer.Bytes()[chunkStart+4] = compressionZlib

		// Write location to table
		chunkEnd = buffer.Len() // Length with padding
		locationBytes := binary.BigEndian.AppendUint32(nil, uint32(chunkStart/sectorLen))[1:4]
		copy(buffer.Bytes()[i*headerEntrySize:], locationBytes)
		buffer.Bytes()[(i*headerEntrySize)+3] = byte((chunkEnd - chunkStart) / sectorLen)

		// Write timestamp to table
		binary.BigEndian.PutUint32(
			buffer.Bytes()[(i*headerEntrySize)+sectorLen:],
			uint32(r.Timestamps[i].Unix()))
	}

	_, err := buffer.WriteTo(e.writer)
	return err
}
