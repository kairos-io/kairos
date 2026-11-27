package p2p

import (
	"encoding/base64"
	"fmt"
	"sync"
)

// Bitmap tells which chunks of an image a node holds, verified.
type Bitmap struct {
	mu   sync.RWMutex
	n    int
	bits []byte
}

// NewBitmap returns an empty bitmap of n chunks.
func NewBitmap(n int) *Bitmap { return &Bitmap{n: n, bits: make([]byte, (n+7)/8)} }

// Has tells if chunk i is held.
func (b *Bitmap) Has(i int) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return i >= 0 && i < b.n && b.bits[i/8]&(1<<(i%8)) != 0
}

// Set marks chunk i as held.
func (b *Bitmap) Set(i int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if i >= 0 && i < b.n {
		b.bits[i/8] |= 1 << (i % 8)
	}
}

// Clear marks chunk i as missing.
func (b *Bitmap) Clear(i int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if i >= 0 && i < b.n {
		b.bits[i/8] &^= 1 << (i % 8)
	}
}

// Count returns how many chunks are held.
func (b *Bitmap) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	c := 0
	for i := 0; i < b.n; i++ {
		if b.bits[i/8]&(1<<(i%8)) != 0 {
			c++
		}
	}
	return c
}

// Len returns the number of chunks the bitmap covers.
func (b *Bitmap) Len() int { return b.n }

// Encode returns the base64 form sent over the wire.
func (b *Bitmap) Encode() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return base64.StdEncoding.EncodeToString(b.bits)
}

// DecodeBitmap parses the wire form for a bitmap of n chunks.
func DecodeBitmap(s string, n int) (*Bitmap, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, err
	}
	if len(raw) != (n+7)/8 {
		return nil, fmt.Errorf("bitmap has %d bytes, %d expected", len(raw), (n+7)/8)
	}
	return &Bitmap{n: n, bits: raw}, nil
}

// fullBitmap returns a bitmap with every chunk held.
func fullBitmap(n int) *Bitmap {
	b := NewBitmap(n)
	for i := 0; i < n; i++ {
		b.Set(i)
	}
	return b
}
