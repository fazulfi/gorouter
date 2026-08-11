package soak

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrInvalidDuration    = errors.New("duration must be positive")
	ErrInvalidInterval    = errors.New("sample interval must be >= 1s")
	ErrInvalidConcurrency = errors.New("concurrency must be >= 1")
	ErrInvalidAuthRatio   = errors.New("auth ratio must be in [0,1]")
)

type Config struct {
	Duration           time.Duration
	SampleInterval     time.Duration
	Concurrency        int
	FailClosed         bool
	TestMode           bool
	ProviderCount      int
	AuthRequestRatio   float64
	MemoryThresholdMB  int64
	GoroutineThreshold int64
}

func (c Config) Validate() error {
	if c.Duration <= 0 {
		return ErrInvalidDuration
	}
	if c.SampleInterval < 1*time.Second {
		return ErrInvalidInterval
	}
	if c.Concurrency < 1 {
		return ErrInvalidConcurrency
	}
	if c.AuthRequestRatio < 0 || c.AuthRequestRatio > 1 {
		return ErrInvalidAuthRatio
	}
	return nil
}

type Metrics struct {
	Rate          float64 `json:"rate"`
	ErrorRate     float64 `json:"error_rate"`
	LatencyP95Ms  float64 `json:"latency_p95_ms"`
	Goroutines    int     `json:"goroutines"`
	MemoryBytes   int64   `json:"memory_bytes"`
	TimeElapsed   float64 `json:"time_elapsed"`
	RouterHits    int64   `json:"router_hits"`
	StreamingHits int64   `json:"streaming_hits"`
	AuthHits      int64   `json:"auth_hits"`
	ProviderCalls int64   `json:"provider_calls"`
}

type Harness struct {
	config        Config
	targetURL     string
	samplesChan   chan *TrendPoint
	mu            sync.RWMutex
	started       atomic.Bool
	stopOnce      sync.Once
	stopChan      chan struct{}
	rng           *rand.Rand
	wg            sync.WaitGroup
	samplerWg     sync.WaitGroup
	routerHits    atomic.Int64
	streamingHits atomic.Int64
	authHits      atomic.Int64
	providerCalls atomic.Int64
	errorCount    atomic.Int64
	requestCount  atomic.Int64
	startTime     time.Time
	client        *http.Client

	latMu     sync.RWMutex
	latencies []float64

	metricsMu   sync.RWMutex
	lastMetrics Metrics
}

func NewHarness(cfg Config) (*Harness, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Harness{
		config:      cfg,
		samplesChan: make(chan *TrendPoint, 1000),
		stopChan:    make(chan struct{}),
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		latencies: make([]float64, 0, 4096),
	}, nil
}

func IsHarnessReady() bool { return true }

func (h *Harness) SetTarget(url string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.targetURL = url
}

func (h *Harness) target() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.targetURL
}

func (h *Harness) GetMetrics() Metrics {
	h.metricsMu.RLock()
	defer h.metricsMu.RUnlock()
	return h.lastMetrics
}

func (h *Harness) Samples() <-chan *TrendPoint { return h.samplesChan }

func (h *Harness) Start() {
	if h.started.Swap(true) {
		return
	}
	h.startTime = time.Now()
	for i := 0; i < h.config.Concurrency; i++ {
		h.wg.Add(1)
		go h.worker(i)
	}
	h.samplerWg.Add(1)
	go h.sampler()
}

func (h *Harness) sampler() {
	defer h.samplerWg.Done()
	ticker := time.NewTicker(h.config.SampleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-h.stopChan:
			close(h.samplesChan)
			return
		case <-ticker.C:
			snap := h.collectSnapshot()
			select {
			case h.samplesChan <- snap:
			default:
			}
		}
	}
}

func (h *Harness) collectSnapshot() *TrendPoint {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return &TrendPoint{
		Timestamp:          time.Now(),
		LatencyPercentiles: h.percentiles(),
		ErrorCount:         h.errorCount.Load(),
		GoroutineCount:     int64(runtime.NumGoroutine()),
		MemoryBytes:        int64(m.HeapAlloc),
	}
}

func (h *Harness) percentiles() Percentiles {
	h.latMu.RLock()
	defer h.latMu.RUnlock()
	if len(h.latencies) == 0 {
		return Percentiles{}
	}
	s := make([]float64, len(h.latencies))
	copy(s, h.latencies)
	sort.Float64s(s)
	pc := func(p float64) float64 { return s[int(p*float64(len(s)-1))] }
	var sum float64
	for _, v := range s {
		sum += v
	}
	return Percentiles{P50Ms: pc(0.50), P95Ms: pc(0.95), P99Ms: pc(0.99), AvgMs: sum / float64(len(s))}
}

func (h *Harness) worker(id int) {
	defer h.wg.Done()
	target := h.target()
	if target == "" {
		return
	}
	for {
		select {
		case <-h.stopChan:
			return
		default:
		}
		h.runWorkload(target, id)
	}
}

func (h *Harness) runWorkload(target string, workerID int) {
	workloadType := h.rng.Float64()
	start := time.Now()
	var err error
	switch {
	case workloadType < 0.60:
		h.routerHits.Add(1)
		err = h.httpRequest(target)
	case workloadType < 0.85:
		h.streamingHits.Add(1)
		err = h.streamRequest(target)
	case workloadType < 0.90:
		h.providerCalls.Add(1)
		err = h.providerRequest(target, workerID)
	default:
		h.authHits.Add(1)
		err = h.authRequest(target)
	}
	elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0
	h.requestCount.Add(1)
	if err != nil {
		h.errorCount.Add(1)
	} else {
		h.recordLatency(elapsedMs)
	}
	h.updateMetrics()
}

func (h *Harness) httpRequest(target string) error {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (h *Harness) streamRequest(target string) error {
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := h.client.Do(req.WithContext(ctx))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	buf := make([]byte, 256)
	io.ReadFull(resp.Body, buf)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected stream status %d", resp.StatusCode)
	}
	return nil
}

func (h *Harness) providerRequest(target string, workerID int) error {
	n := h.config.ProviderCount
	if n < 1 {
		n = 1
	}
	url := target + "/provider/" + fmt.Sprint(workerID%n)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}

func (h *Harness) authRequest(target string) error {
	url := target + "/api/v1/usage"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer test-placeholder-token")
	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return nil
}

func (h *Harness) recordLatency(ms float64) {
	h.latMu.Lock()
	defer h.latMu.Unlock()
	h.latencies = append(h.latencies, ms)
	if len(h.latencies) > 4096 {
		h.latencies = h.latencies[len(h.latencies)-4096:]
	}
}

func (h *Harness) updateMetrics() {
	h.metricsMu.Lock()
	defer h.metricsMu.Unlock()
	m := h.lastMetrics
	m.TimeElapsed = time.Since(h.startTime).Seconds()
	m.RouterHits = h.routerHits.Load()
	m.StreamingHits = h.streamingHits.Load()
	m.AuthHits = h.authHits.Load()
	m.ProviderCalls = h.providerCalls.Load()
	m.Goroutines = runtime.NumGoroutine()
	reqs := h.requestCount.Load()
	if reqs > 0 {
		m.ErrorRate = float64(h.errorCount.Load()) / float64(reqs)
	}
	if m.TimeElapsed > 0.001 {
		m.Rate = float64(reqs) / m.TimeElapsed
	}
	m.LatencyP95Ms = h.percentiles().P95Ms
	h.lastMetrics = m
}

func (h *Harness) Stop() {
	h.stopOnce.Do(func() {
		close(h.stopChan)
		h.wg.Wait()
		h.samplerWg.Wait()
		h.started.Store(false)
	})
}

func (h *Harness) String() string {
	return fmt.Sprintf("soak{concurrency=%d duration=%s interval=%s}",
		h.config.Concurrency, h.config.Duration, h.config.SampleInterval)
}
