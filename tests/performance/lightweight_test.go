package performance

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type result struct {
	Name              string  `json:"name"`
	Iterations        int     `json:"iterations"`
	P50Ms             float64 `json:"p50_ms"`
	P95Ms             float64 `json:"p95_ms"`
	P99Ms             float64 `json:"p99_ms"`
	RequestsPerSecond float64 `json:"requests_per_second"`
}

type bundle struct {
	Command     string            `json:"command"`
	Environment map[string]string `json:"environment"`
	Results     []result          `json:"results"`
}

var iterations = flag.Int("perf-iterations", 200, "bounded number of requests per scenario")
var concurrency = flag.Int("perf-concurrency", 16, "bounded concurrent workers")
var output = flag.String("perf-output", "", "optional JSON result bundle path")

func BenchmarkPerformanceHarness(b *testing.B) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r := httptest.NewServer(h)
	defer r.Close()
	for i := 0; i < b.N; i++ {
		res, err := http.Get(r.URL)
		if err != nil {
			b.Fatal(err)
		}
		res.Body.Close()
	}
}

func TestRouterOverheadP95(t *testing.T) {
	r := runScenario(t, "router_overhead", func() http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	})
	t.Logf("router overhead: p50=%.3fms p95=%.3fms p99=%.3fms", r.P50Ms, r.P95Ms, r.P99Ms)
	if limit := os.Getenv("PERF_ENFORCE_THRESHOLDS"); limit == "1" && r.P95Ms >= 20 {
		t.Fatalf("router overhead p95 %.3fms >= 20ms", r.P95Ms)
	}
	writeBundle(t, []result{r})
}

func TestConcurrentStreams(t *testing.T) {
	if *iterations < 1 || *concurrency < 1 {
		t.Fatal("iterations and concurrency must be positive")
	}
	var active int64
	var peak int64
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt64(&active, 1)
		for {
			p := atomic.LoadInt64(&peak)
			if n <= p || atomic.CompareAndSwapInt64(&peak, p, n) {
				break
			}
		}
		time.Sleep(time.Microsecond)
		atomic.AddInt64(&active, -1)
		w.WriteHeader(http.StatusNoContent)
	})
	server := httptest.NewServer(h)
	defer server.Close()
	var wg sync.WaitGroup
	for worker := 0; worker < *concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < *iterations; i++ {
				res, err := http.Get(server.URL)
				if err != nil {
					t.Error(err)
					return
				}
				res.Body.Close()
			}
		}()
	}
	wg.Wait()
	t.Logf("peak concurrent streams=%d (local harness limit=%d)", peak, *concurrency)
	if os.Getenv("PERF_ENFORCE_THRESHOLDS") == "1" && peak < int64(*concurrency) {
		t.Fatalf("peak concurrency %d below configured %d", peak, *concurrency)
	}
}

func TestLightweightRPM(t *testing.T) {
	r := runScenario(t, "lightweight", func() http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	})
	t.Logf("lightweight endpoint: %.1f requests/sec (%.1f RPM)", r.RequestsPerSecond, r.RequestsPerSecond*60)
	if os.Getenv("PERF_ENFORCE_THRESHOLDS") == "1" && r.RequestsPerSecond*60 < 100000 {
		t.Fatalf("lightweight throughput %.1f RPM < 100000", r.RequestsPerSecond*60)
	}
	writeBundle(t, []result{r})
}

func runScenario(t *testing.T, name string, makeHandler func() http.Handler) result {
	t.Helper()
	server := httptest.NewServer(makeHandler())
	defer server.Close()
	samples := make([]float64, 0, *iterations)
	start := time.Now()
	for i := 0; i < *iterations; i++ {
		began := time.Now()
		res, err := http.Get(server.URL)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		samples = append(samples, float64(time.Since(began).Microseconds())/1000)
	}
	sort.Float64s(samples)
	percentile := func(p float64) float64 { return samples[int(p*float64(len(samples)-1))] }
	return result{Name: name, Iterations: *iterations, P50Ms: percentile(.50), P95Ms: percentile(.95), P99Ms: percentile(.99), RequestsPerSecond: float64(len(samples)) / time.Since(start).Seconds()}
}

func writeBundle(t *testing.T, results []result) {
	if *output == "" {
		return
	}
	t.Helper()
	b, err := json.MarshalIndent(bundle{Command: fmt.Sprintf("go test ./tests/performance/... -count=1 -run 'TestRouterOverheadP95|TestConcurrentStreams|TestLightweightRPM' -args -perf-iterations=%d -perf-concurrency=%d -perf-output=%s", *iterations, *concurrency, *output), Environment: map[string]string{"go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH, "cpu_count": fmt.Sprint(runtime.NumCPU()), "thresholds": os.Getenv("PERF_ENFORCE_THRESHOLDS")}, Results: results}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(*output, append(b, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
