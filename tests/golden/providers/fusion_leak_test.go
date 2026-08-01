package providers

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorouter/internal/domain/combo"
	enginecombos "gorouter/internal/engine/combos"

	"github.com/google/uuid"
)

func leakTestMembers() []combo.Member {
	return []combo.Member{
		{ID: uuid.New(), ComboID: uuid.New(), ProviderID: uuid.New(), ModelRef: "panel-a", Priority: 0, Weight: 1, IsActive: true},
		{ID: uuid.New(), ComboID: uuid.New(), ProviderID: uuid.New(), ModelRef: "panel-b", Priority: 1, Weight: 1, IsActive: true},
		{ID: uuid.New(), ComboID: uuid.New(), ProviderID: uuid.New(), ModelRef: "panel-c", Priority: 2, Weight: 1, IsActive: true},
	}
}

func leakTestConfig(grace, hard string) combo.FusionConfig {
	return combo.FusionConfig{
		Quorum:       2,
		GracePeriod:  grace,
		HardTimeout:  hard,
		JudgeModelID: "judge/model",
	}
}

func runFusionOnce(t *testing.T, mode string) {
	t.Helper()
	cfg := leakTestConfig("30ms", "300ms")
	invoke := func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
		return []byte("panel-ok"), nil
	}
	if mode == "block" || mode == "cancel" {
		invoke = func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
	}
	judge := func(ctx context.Context, panels []enginecombos.PanelResult) ([]byte, error) {
		return []byte("judged"), nil
	}
	if mode == "cancel" {
		cfg = leakTestConfig("59m", "1h")
	}
	ctx := context.Background()
	if mode == "cancel" {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
	}
	_, err := enginecombos.NewFusionEngine().Run(ctx, cfg, leakTestMembers(), []byte("req"), invoke, judge)
	if mode == "cancel" && !errors.Is(err, context.Canceled) {
		t.Errorf("cancel run err = %v, want context.Canceled", err)
	}
	if mode == "block" && !errors.Is(err, enginecombos.ErrFusionHardTimeout) {
		t.Errorf("block run err = %v, want ErrFusionHardTimeout", err)
	}
}

// TestGolden_FusionNoGoroutineLeaks drives the fusion engine through
// completion, hard-timeout, and cancellation cycles and verifies every panel
// and judge goroutine is joined before the next run starts.
func TestGolden_FusionNoGoroutineLeaks(t *testing.T) {
	for i := 0; i < 5; i++ {
		runFusionOnce(t, "ok")
	}
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	for i := 0; i < 15; i++ {
		runFusionOnce(t, "ok")
	}
	for i := 0; i < 8; i++ {
		runFusionOnce(t, "block")
	}
	for i := 0; i < 15; i++ {
		runFusionOnce(t, "cancel")
	}

	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > baseline+3 {
		t.Errorf("goroutine leak: baseline=%d after=%d", baseline, after)
	}
}

// TestGolden_FusionConcurrentRunsRaceSafe launches many fusion runs in
// parallel; the engine must serialize its fan-out per run without shared-state
// races (run with -race).
func TestGolden_FusionConcurrentRunsRaceSafe(t *testing.T) {
	const runs = 8
	var failed atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			cfg := leakTestConfig("20ms", "300ms")
			invoke := func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
				return []byte("ok"), nil
			}
			judge := func(ctx context.Context, panels []enginecombos.PanelResult) ([]byte, error) {
				return []byte("judged"), nil
			}
			_, err := enginecombos.NewFusionEngine().Run(context.Background(), cfg, leakTestMembers(), []byte("req"), invoke, judge)
			if err != nil {
				failed.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if failed.Load() != 0 {
		t.Errorf("%d concurrent fusion runs failed", failed.Load())
	}
}
