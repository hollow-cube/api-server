package util

import "encoding/base64"

// BitSet represents a set of bits
type BitSet struct {
	data []byte
}

// NewBitSet creates a new BitSet with enough space for `size` bits
func NewBitSet(size int) *BitSet {
	return &BitSet{
		data: make([]byte, (size+7)/8), // Each uint64 stores 64 bits
	}
}

// Set sets the bit at position `n`
func (b *BitSet) Set(n int) {
	idx, pos := n/8, uint(n%8)
	b.ensureCapacity(idx)
	b.data[idx] |= 1 << pos
}

// String returns the bitset as a base64 string
func (b *BitSet) String() string {
	return base64.StdEncoding.EncodeToString(b.data)
}

// ensureCapacity ensures the slice has at least `size` elements
func (b *BitSet) ensureCapacity(size int) {
	if size >= len(b.data) {
		newData := make([]byte, size+1)
		copy(newData, b.data)
		b.data = newData
	}
}
