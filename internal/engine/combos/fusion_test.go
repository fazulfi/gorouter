package combos

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gorouter/internal/domain/combo"
)

func fusionCfg(quorum int, grace, hard string, judgeModel string) combo.FusionConfig {
	return combo.FusionConfig{
		Quorum:       quorum,
		GracePeriod:  grace,
		HardTimeout:  hard,
		JudgeModelID: judgeModel,
	}
}

func okInvoker(body string) Invoker {
	return func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
		return []byte(body + ":" + m.ModelRef), nil
	}
}

func failInvoker(msg string) Invoker {
	return func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
		return nil, errors.New(msg)
	}
}

func blockingInvoker() Invoker {
	return func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
}

func noopJudge() Judge {
	return func(ctx context.Context, panels []PanelResult) ([]byte, error) {
		return []byte("judged"), nil
	}
}

func TestFusion_ZeroSuccessReturnsError(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	_, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(2, "50ms", "500ms", "judge/model"),
		members, testRequest, failInvoker("panel down"), noopJudge())
	if !errors.Is(err, ErrFusionNoSuccess) {
		t.Fatalf("err = %v, want ErrFusionNoSuccess", err)
	}
	if !strings.Contains(err.Error(), "panel down") {
		t.Errorf("err should include panel error: %v", err)
	}
}

func TestFusion_OneSuccessDirectNoJudge(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b"), testMember("c")}
	var judgeCalls atomic.Int32
	judge := func(ctx context.Context, panels []PanelResult) ([]byte, error) {
		judgeCalls.Add(1)
		return []byte("judged"), nil
	}
	res, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(2, "1s", "2s", "judge/model"),
		members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			if m.ModelRef == "a" {
				return []byte("direct-a"), nil
			}
			return nil, errors.New("down")
		}, judge)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Body) != "direct-a" {
		t.Errorf("body = %s, want direct-a", res.Body)
	}
	if judgeCalls.Load() != 0 {
		t.Error("judge must not run with a single success")
	}
}

func TestFusion_QuorumJudgeReceivesLiveContext(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	judge := func(ctx context.Context, panels []PanelResult) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("judge context not live: %w", err)
		}
		if len(panels) != 2 {
			t.Errorf("judge panels = %d, want 2", len(panels))
		}
		return []byte("synthesized"), nil
	}
	res, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(2, "200ms", "2s", "judge/model"),
		members, testRequest, okInvoker("ok"), judge)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Body) != "synthesized" {
		t.Errorf("body = %s, want synthesized", res.Body)
	}
}

func TestFusion_QuorumJudgeCalledOnce(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b"), testMember("c")}
	var judgeCalls atomic.Int32
	judge := func(ctx context.Context, panels []PanelResult) ([]byte, error) {
		judgeCalls.Add(1)
		if len(panels) != 2 {
			t.Errorf("judge panels = %d, want 2", len(panels))
		}
		return []byte("synthesized"), nil
	}
	res, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(2, "500ms", "2s", "judge/model"),
		members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			if m.ModelRef == "c" {
				return nil, errors.New("c down")
			}
			return []byte("ok-" + m.ModelRef), nil
		}, judge)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Body) != "synthesized" {
		t.Errorf("body = %s, want synthesized", res.Body)
	}
	if judgeCalls.Load() != 1 {
		t.Errorf("judge calls = %d, want 1", judgeCalls.Load())
	}
}

func TestFusion_JudgePanelsDeterministicOrder(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	order := []string{}
	judge := func(ctx context.Context, panels []PanelResult) ([]byte, error) {
		for _, p := range panels {
			order = append(order, p.Member.ModelRef)
		}
		return []byte("synthesized"), nil
	}
	res, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(2, "300ms", "2s", "judge/model"),
		members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			if m.ModelRef == "a" {
				select {
				case <-time.After(60 * time.Millisecond):
					return []byte("ok-a"), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return []byte("ok-b"), nil
		}, judge)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Body) != "synthesized" {
		t.Errorf("body = %s", res.Body)
	}
	// b completes before a, but judge must receive member order a,b.
	if len(order) != 2 || order[0] != "a" || order[1] != "b" {
		t.Errorf("judge order = %v, want [a b]", order)
	}
}

func TestFusion_QuorumNotReachedGraceExpiresDirect(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	var judgeCalls atomic.Int32
	start := time.Now()
	res, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(2, "80ms", "2s", "judge/model"),
		members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			if m.ModelRef == "a" {
				return []byte("direct-a"), nil
			}
			return blockingInvoker()(ctx, m, req)
		}, func(ctx context.Context, panels []PanelResult) ([]byte, error) {
			judgeCalls.Add(1)
			return nil, nil
		})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Body) != "direct-a" {
		t.Errorf("body = %s, want direct-a", res.Body)
	}
	if judgeCalls.Load() != 0 {
		t.Error("judge must not run below quorum")
	}
	if elapsed < 60*time.Millisecond {
		t.Errorf("returned too early (%v), grace period must elapse", elapsed)
	}
	if elapsed > 1*time.Second {
		t.Errorf("returned too late (%v)", elapsed)
	}
}

func TestFusion_WaitsGraceForSlowPanel(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	var judgeCalls atomic.Int32
	res, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(2, "200ms", "2s", "judge/model"),
		members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			if m.ModelRef == "a" {
				return []byte("ok-a"), nil
			}
			select {
			case <-time.After(100 * time.Millisecond):
				return []byte("ok-b"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}, func(ctx context.Context, panels []PanelResult) ([]byte, error) {
			judgeCalls.Add(1)
			return []byte("synthesized"), nil
		})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Body) != "synthesized" {
		t.Errorf("body = %s, want synthesized", res.Body)
	}
	if judgeCalls.Load() != 1 {
		t.Errorf("judge calls = %d, want 1", judgeCalls.Load())
	}
}

func TestFusion_AllPanelsDoneBeforeGraceFinalizesImmediately(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	start := time.Now()
	res, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(2, "1s", "2s", "judge/model"),
		members, testRequest, okInvoker("ok"), noopJudge())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Body) != "judged" {
		t.Errorf("body = %s, want judged", res.Body)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("should finalize when all panels done: %v", elapsed)
	}
}

func TestFusion_HardTimeout(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	start := time.Now()
	_, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(2, "100ms", "120ms", "judge/model"),
		members, testRequest, blockingInvoker(), noopJudge())
	elapsed := time.Since(start)
	if !errors.Is(err, ErrFusionHardTimeout) {
		t.Fatalf("err = %v, want ErrFusionHardTimeout", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("hard timeout returned too late: %v", elapsed)
	}
}

func TestFusion_CancellationPropagates(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	started := make(chan struct{}, len(members))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_, err := NewFusionEngine().Run(ctx,
		fusionCfg(2, "59m", "1h", "judge/model"),
		members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			started <- struct{}{}
			return blockingInvoker()(ctx, m, req)
		}, noopJudge())
	elapsed := time.Since(start)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("cancellation returned too late: %v", elapsed)
	}
}

func TestFusion_JudgeFailure(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	_, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(2, "500ms", "2s", "judge/model"),
		members, testRequest, okInvoker("ok"),
		func(ctx context.Context, panels []PanelResult) ([]byte, error) {
			return nil, errors.New("judge exploded")
		})
	if !errors.Is(err, ErrJudgeFailed) {
		t.Fatalf("err = %v, want ErrJudgeFailed", err)
	}
	if !strings.Contains(err.Error(), "judge exploded") {
		t.Errorf("err should wrap judge error: %v", err)
	}
}

func TestFusion_InvalidConfigFailsBeforeInvocation(t *testing.T) {
	members := []combo.Member{testMember("a")}
	var invoked atomic.Int32
	invoke := func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
		invoked.Add(1)
		return []byte("x"), nil
	}
	tests := []combo.FusionConfig{
		{},
		{Quorum: 2, GracePeriod: "8s", HardTimeout: "90s"},
		{Quorum: 2, GracePeriod: "x", HardTimeout: "90s", JudgeModelID: "m"},
		{Quorum: 2, GracePeriod: "90s", HardTimeout: "90s", JudgeModelID: "m"},
	}
	for i, cfg := range tests {
		_, err := NewFusionEngine().Run(context.Background(), cfg, members, testRequest, invoke, noopJudge())
		if err == nil {
			t.Errorf("case %d: expected config error", i)
		}
	}
	if invoked.Load() != 0 {
		t.Errorf("panels invoked %d times, want 0", invoked.Load())
	}
}

func TestFusion_NoEligibleMember(t *testing.T) {
	_, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(2, "50ms", "500ms", "judge/model"),
		[]combo.Member{{IsActive: false}}, testRequest, okInvoker("ok"), noopJudge())
	if !errors.Is(err, ErrNoEligibleMember) {
		t.Fatalf("err = %v, want ErrNoEligibleMember", err)
	}
}

func TestFusion_NoGoroutineLeaks(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b"), testMember("c")}

	runOnce := func(block bool) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		invoke := Invoker(okInvoker("ok"))
		if block {
			invoke = blockingInvoker()
		}
		_, _ = NewFusionEngine().Run(ctx,
			fusionCfg(2, "30ms", "300ms", "judge/model"),
			members, testRequest, invoke, noopJudge())
	}

	for i := 0; i < 5; i++ {
		runOnce(false)
	}
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		runOnce(false)
	}
	for i := 0; i < 10; i++ {
		runOnce(true)
	}
	for i := 0; i < 20; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		_, _ = NewFusionEngine().Run(ctx,
			fusionCfg(2, "59m", "1h", "judge/model"),
			members, testRequest, blockingInvoker(), noopJudge())
	}

	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	after := runtime.NumGoroutine()
	if after > baseline+3 {
		t.Errorf("goroutine leak: baseline=%d after=%d", baseline, after)
	}
}

func TestFusion_SinglePanelsConfig(t *testing.T) {
	members := []combo.Member{testMember("solo")}
	res, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(1, "50ms", "500ms", "judge/model"),
		members, testRequest, okInvoker("ok"), noopJudge())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Body) != "ok:solo" {
		t.Errorf("body = %s, want direct solo result", res.Body)
	}
}

func TestFusion_MultipleResultsFormatted(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b"), testMember("c")}
	var judgeCalls atomic.Int32
	_, err := NewFusionEngine().Run(context.Background(),
		fusionCfg(2, "200ms", "2s", "judge/model"),
		members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			if m.ModelRef == "c" {
				return nil, fmt.Errorf("c failed")
			}
			return []byte("ok"), nil
		}, func(ctx context.Context, panels []PanelResult) ([]byte, error) {
			judgeCalls.Add(1)
			if len(panels) != 2 {
				t.Errorf("panels = %d, want 2", len(panels))
			}
			return []byte("judged"), nil
		})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if judgeCalls.Load() != 1 {
		t.Errorf("judge calls = %d, want 1", judgeCalls.Load())
	}
}

// TestFusion_HardTimeoutDeterministicWhenPanelsCutAtDeadline is the P3-T10
// regression: every panel is cut off by its context deadline at the hard
// timeout instant, so the results channel closes at the same moment the hard
// timer fires. The hard deadline is the absolute bound — the terminal outcome
// must be ErrFusionHardTimeout, never ErrFusionNoSuccess, no matter which
// select case the collection loop exits through. Against the pre-fix code
// the closed results channel can win the select first, leaving timedOut
// false and misclassifying the outcome as ErrFusionNoSuccess. Iterations run
// in parallel so cross-run scheduling contention reproduces the race
// reliably instead of depending on machine load.
func TestFusion_HardTimeoutDeterministicWhenPanelsCutAtDeadline(t *testing.T) {
	members := make([]combo.Member, 32)
	for i := range members {
		members[i] = testMember(fmt.Sprintf("p%d", i))
	}
	const (
		workers    = 4
		iterations = 100
	)
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		miscl []string
	)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				_, err := NewFusionEngine().Run(context.Background(),
					fusionCfg(2, "5ms", "25ms", "judge/model"),
					members, testRequest, blockingInvoker(), noopJudge())
				if !errors.Is(err, ErrFusionHardTimeout) {
					mu.Lock()
					miscl = append(miscl, fmt.Sprintf("iteration %d: %v", i, err))
					mu.Unlock()
					return
				}
			}
		}()
	}
	wg.Wait()
	if len(miscl) > 0 {
		t.Fatalf("hard deadline fired but %d outcome(s) misclassified; first: %s (want ErrFusionHardTimeout)",
			len(miscl), miscl[0])
	}
}
