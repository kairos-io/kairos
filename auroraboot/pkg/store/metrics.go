package store

import "time"

// NodeMetrics is one telemetry sample that an agent sends with its heartbeat.
// Every field is optional.
type NodeMetrics struct {
	SampledAt     time.Time      `json:"sampledAt"`
	UptimeSeconds uint64         `json:"uptimeSeconds,omitempty"`
	Load          []float64      `json:"load,omitempty"`
	CPU           *CPUMetrics    `json:"cpu,omitempty"`
	Memory        *MemoryMetrics `json:"memory,omitempty"`
	Disks         []DiskMetrics  `json:"disks,omitempty"`
	TemperatureC  *float64       `json:"temperatureC,omitempty"`
}

// CPUMetrics holds the CPU usage of a node.
type CPUMetrics struct {
	UsedPercent float64 `json:"usedPercent"`
}

// MemoryMetrics holds the memory usage of a node.
type MemoryMetrics struct {
	TotalBytes     uint64 `json:"totalBytes"`
	AvailableBytes uint64 `json:"availableBytes"`
}

// DiskMetrics holds the usage of one mounted disk of a node.
type DiskMetrics struct {
	Label      string `json:"label,omitempty"`
	Mount      string `json:"mount"`
	TotalBytes uint64 `json:"totalBytes"`
	UsedBytes  uint64 `json:"usedBytes"`
}
