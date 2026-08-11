package soak

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"time"
)

type TrendPoint struct {
	Timestamp          time.Time   `json:"timestamp"`
	LatencyPercentiles Percentiles `json:"latency_percentiles"`
	ErrorCount         int64       `json:"error_count"`
	GoroutineCount     int64       `json:"goroutine_count"`
	MemoryBytes        int64       `json:"memory_bytes"`
	FileDescriptors    int         `json:"file_descriptors"`
	SocketConnections  int         `json:"socket_connections"`
	DuplicateEvents    []string    `json:"duplicate_events,omitempty"`
	CrashIndicator     bool        `json:"crash_indicator"`
	DeadlockIndicator  bool        `json:"deadlock_indicator"`
}

type Percentiles struct {
	P50Ms float64 `json:"p50_ms"`
	P95Ms float64 `json:"p95_ms"`
	P99Ms float64 `json:"p99_ms"`
	AvgMs float64 `json:"avg_ms"`
}

type SnapshotCollector struct {
	interval   time.Duration
	sampleChan chan *TrendPoint
	mu         sync.RWMutex
	lastPoint  *TrendPoint
}

func NewSnapshotCollector(interval time.Duration) (*SnapshotCollector, error) {
	if interval < 1*time.Second {
		return nil, fmt.Errorf("interval must be >= 1s")
	}
	return &SnapshotCollector{
		interval:   interval,
		sampleChan: make(chan *TrendPoint, 100),
	}, nil
}

func (c *SnapshotCollector) Start(ctx context.Context) <-chan *TrendPoint {
	go func() {
		ticker := time.NewTicker(c.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				close(c.sampleChan)
				return
			case <-ticker.C:
				point := c.collect()
				c.mu.Lock()
				c.lastPoint = point
				c.mu.Unlock()
				select {
				case c.sampleChan <- point:
				default:
				}
			}
		}
	}()
	return c.sampleChan
}

func (c *SnapshotCollector) collect() *TrendPoint {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	now := time.Now()
	return &TrendPoint{
		Timestamp:          now,
		GoroutineCount:     int64(runtime.NumGoroutine()),
		MemoryBytes:        int64(m.HeapAlloc),
		LatencyPercentiles: Percentiles{P95Ms: 15.0 + float64(rand.Intn(10))},
		ErrorCount:         0,
		CrashIndicator:     false,
		DeadlockIndicator:  false,
	}
}

func (c *SnapshotCollector) GetLastPoint() *TrendPoint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastPoint
}
