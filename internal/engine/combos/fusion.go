package combos

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/combo"
)

// FusionEngine runs all combo members as concurrent panels and applies the
// upstream degradation contract: zero successes produce an error, one success
// returns the panel result directly, and quorum of successes (default 2) goes
// to judge synthesis. The grace period (default 8s) starts at the first panel
// success; the hard timeout (default 90s) is the absolute deadline. Every
// goroutine is joined before Run returns — no leaks, exactly one terminal
// outcome, and parent cancellation propagates to every panel.
type FusionEngine struct{}

// NewFusionEngine creates a fusion engine.
func NewFusionEngine() *FusionEngine { return &FusionEngine{} }

// Run fans out panels with the request body and returns the single terminal
// outcome.
func (e *FusionEngine) Run(ctx context.Context, cfg combo.FusionConfig, members []combo.Member, request []byte, invoke Invoker, judge Judge) (*PanelResult, error) {
	active := combo.ActiveMembers(members)
	if len(active) == 0 {
		return nil, ErrNoEligibleMember
	}
	cfg = cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", combo.ErrInvalidConfig, err)
	}
	grace, err := cfg.Grace()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", combo.ErrInvalidConfig, err)
	}
	hard, err := cfg.Timeout()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", combo.ErrInvalidConfig, err)
	}
	quorum := cfg.EffectiveQuorum()

	parent := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	// hardDeadline is the single authoritative hard-timeout instant: the
	// panel contexts, the judge context, and the collection timer all derive
	// from it, so once it fires the terminal outcome is deterministically
	// ErrFusionHardTimeout no matter which select case wins the race.
	hardDeadline := time.Now().Add(hard)
	hardCtx, hardCancel := context.WithDeadline(ctx, hardDeadline)
	defer hardCancel()
	// The judge context derives from the ORIGINAL parent with the same hard
	// deadline, so cancelling panels below never cancels the judge's context;
	// parent cancellation and the hard deadline still bound it.
	judgeCtx, judgeCancel := context.WithDeadline(parent, hardDeadline)
	defer judgeCancel()

	results := make(chan PanelResult, len(active))
	var wg sync.WaitGroup
	for _, m := range active {
		m := m
		wg.Add(1)
		go func() {
			defer wg.Done()
			body, err := invoke(hardCtx, m, request)
			results <- PanelResult{Member: m, Body: body, Err: err}
		}()
	}
	closed := make(chan struct{})
	go func() {
		wg.Wait()
		close(results)
		close(closed)
	}()

	var successes, failures []PanelResult
	var graceTimer *time.Timer
	var graceC <-chan time.Time
	hardTimer := time.NewTimer(time.Until(hardDeadline))
	defer hardTimer.Stop()
	timedOut := false

collect:
	for {
		select {
		case <-ctx.Done():
			cancel()
			<-closed
			return nil, ctx.Err()
		case <-hardTimer.C:
			timedOut = true
			break collect
		case r, ok := <-results:
			if !ok {
				break collect
			}
			if r.Err != nil {
				failures = append(failures, r)
				continue
			}
			successes = append(successes, r)
			if len(successes) == 1 {
				graceTimer = time.NewTimer(grace)
				graceC = graceTimer.C
			}
			if len(successes) >= quorum {
				break collect
			}
		case <-graceC:
			break collect
		}
	}
	// The hard deadline is the absolute bound: if it has fired, the outcome
	// is a hard timeout regardless of which case closed the loop (the results
	// channel can close first at the same instant).
	if !time.Now().Before(hardDeadline) {
		timedOut = true
	}
	if graceTimer != nil {
		graceTimer.Stop()
	}
	cancel()
	<-closed

	switch {
	case timedOut:
		return nil, ErrFusionHardTimeout
	case len(successes) == 0:
		return nil, fusionNoSuccess(failures)
	case len(successes) < quorum || len(successes) == 1:
		return &PanelResult{Member: successes[0].Member, Body: successes[0].Body}, nil
	default:
		ordered := orderByMember(active, successes)
		out, err := judge(judgeCtx, ordered)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrJudgeFailed, err)
		}
		return &PanelResult{Body: out, Synthesized: true}, nil
	}
}

func fusionNoSuccess(failures []PanelResult) error {
	errs := make([]error, 0, len(failures)+1)
	errs = append(errs, ErrFusionNoSuccess)
	for _, f := range failures {
		errs = append(errs, fmt.Errorf("panel %s: %w", f.Member.ModelRef, f.Err))
	}
	return errors.Join(errs...)
}

func orderByMember(members []combo.Member, successes []PanelResult) []PanelResult {
	pos := make(map[uuid.UUID]int, len(members))
	for i, m := range members {
		pos[m.ID] = i
	}
	ordered := make([]PanelResult, len(successes))
	copy(ordered, successes)
	sort.SliceStable(ordered, func(i, j int) bool {
		return pos[ordered[i].Member.ID] < pos[ordered[j].Member.ID]
	})
	return ordered
}
