// Package metrics keeps recent node telemetry samples in memory.
package metrics

import (
	"sync"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/store"
)

// DefaultCapacity is the number of samples kept per node when no valid
// capacity is given.
const DefaultCapacity = 120

// MaxDisks and MaxLoad cap the disks and load averages kept from one sample,
// so an oversized sample cannot grow memory.
const (
	MaxDisks = 16
	MaxLoad  = 3
)

// Recorder records a metrics sample for a node.
type Recorder interface {
	Record(nodeID string, m store.NodeMetrics)
}

// Buffer keeps the last samples of each node in memory. Samples are lost on
// restart. It is safe for concurrent use.
type Buffer struct {
	mu       sync.RWMutex
	capacity int
	samples  map[string][]store.NodeMetrics
}

// NewBuffer returns a Buffer that keeps up to capacity samples per node.
// A capacity below 1 means DefaultCapacity.
func NewBuffer(capacity int) *Buffer {
	if capacity < 1 {
		capacity = DefaultCapacity
	}
	return &Buffer{
		capacity: capacity,
		samples:  make(map[string][]store.NodeMetrics),
	}
}

// Record stores a sample for nodeID. A zero SampledAt is set to the current
// UTC time. Disks beyond MaxDisks and load averages beyond MaxLoad are
// dropped. The oldest sample is dropped when the node is at capacity.
func (b *Buffer) Record(nodeID string, m store.NodeMetrics) {
	if m.SampledAt.IsZero() {
		m.SampledAt = time.Now().UTC()
	}
	if len(m.Disks) > MaxDisks {
		m.Disks = m.Disks[:MaxDisks]
	}
	if len(m.Load) > MaxLoad {
		m.Load = m.Load[:MaxLoad]
	}
	m = clone(m)

	b.mu.Lock()
	defer b.mu.Unlock()
	s := append(b.samples[nodeID], m)
	if len(s) > b.capacity {
		// Copy into a new slice so the dropped samples can be collected.
		s = append([]store.NodeMetrics(nil), s[len(s)-b.capacity:]...)
	}
	b.samples[nodeID] = s
}

// Latest returns a copy of the most recent sample of nodeID.
func (b *Buffer) Latest(nodeID string) (store.NodeMetrics, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	s := b.samples[nodeID]
	if len(s) == 0 {
		return store.NodeMetrics{}, false
	}
	return clone(s[len(s)-1]), true
}

// Samples returns a copy of the samples of nodeID, oldest first.
func (b *Buffer) Samples(nodeID string) []store.NodeMetrics {
	b.mu.RLock()
	defer b.mu.RUnlock()
	s := b.samples[nodeID]
	out := make([]store.NodeMetrics, len(s))
	for i := range s {
		out[i] = clone(s[i])
	}
	return out
}

// AllLatest returns a copy of the most recent sample of every node.
func (b *Buffer) AllLatest() map[string]store.NodeMetrics {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make(map[string]store.NodeMetrics, len(b.samples))
	for id, s := range b.samples {
		if len(s) > 0 {
			out[id] = clone(s[len(s)-1])
		}
	}
	return out
}

// Forget removes all samples of nodeID.
func (b *Buffer) Forget(nodeID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.samples, nodeID)
}

// clone returns a deep copy of m, so that callers never share slices or
// pointers with the buffer.
func clone(m store.NodeMetrics) store.NodeMetrics {
	if m.Load != nil {
		m.Load = append([]float64(nil), m.Load...)
	}
	if m.Disks != nil {
		m.Disks = append([]store.DiskMetrics(nil), m.Disks...)
	}
	if m.CPU != nil {
		c := *m.CPU
		m.CPU = &c
	}
	if m.Memory != nil {
		c := *m.Memory
		m.Memory = &c
	}
	if m.TemperatureC != nil {
		t := *m.TemperatureC
		m.TemperatureC = &t
	}
	return m
}
