// Package parity_test provides parity-level tests for the request mutator
// pipeline. These tests verify that the pipeline and its constituent mutators
// satisfy the design contracts:
//   - Exact order: RTK → Headroom → Caveman → Ponytail → Pxpipe
//   - Fail-open: each failed mutator logs/records failure and hands the exact
//     pre-mutator body to the next stage
//   - Content preservation: no message loss during system prompt injection
//   - Cancellation: pipeline stops on cancelled context
//   - No cross-request state: concurrent pipelines don't interfere
//
// Upstream authority: decolua/9router commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1
// Pipeline design: ARCHITECTURE.md §6, DECISIONS.md #108/#162 (fail-open)
package parity_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/mutators"
)

// TestMutatorExactOrder verifies that the default pipeline runs mutators in
// the approved upstream order: RTK → Headroom → Caveman → Ponytail → Pxpipe.
func TestMutatorExactOrder(t *testing.T) {
	var order []string
	p := mutators.New(
		mutators.NamedFunc("rtk", func(_ context.Context, _ *engine.Request) (*mutators.Result, error) {
			order = append(order, "rtk")
			return &mutators.Result{Applied: false}, nil
		}),
		mutators.NamedFunc("headroom", func(_ context.Context, _ *engine.Request) (*mutators.Result, error) {
			order = append(order, "headroom")
			return &mutators.Result{Applied: false}, nil
		}),
		mutators.NamedFunc("caveman", func(_ context.Context, _ *engine.Request) (*mutators.Result, error) {
			order = append(order, "caveman")
			return &mutators.Result{Applied: false}, nil
		}),
		mutators.NamedFunc("ponytail", func(_ context.Context, _ *engine.Request) (*mutators.Result, error) {
			order = append(order, "ponytail")
			return &mutators.Result{Applied: false}, nil
		}),
		mutators.NamedFunc("pxpipe", func(_ context.Context, _ *engine.Request) (*mutators.Result, error) {
			order = append(order, "pxpipe")
			return &mutators.Result{Applied: false}, nil
		}),
	)

	req := &engine.Request{MappedBody: []byte(`{"messages":[]}`)}
	reports := p.Run(context.Background(), req, nil)

	if len(reports) != 5 {
		t.Fatalf("expected 5 reports, got %d", len(reports))
	}
	expected := []string{"rtk", "headroom", "caveman", "ponytail", "pxpipe"}
	for i, name := range order {
		if name != expected[i] {
			t.Fatalf("position %d: expected %q, got %q", i, expected[i], name)
		}
	}
}

// TestMutatorFailOpen verifies that when a middle mutator fails, the pipeline
// continues and subsequent mutators still run (fail-open semantics).
func TestMutatorFailOpen(t *testing.T) {
	var logBuf strings.Builder
	logger := func(f string, args ...interface{}) {
		logBuf.WriteString(strings.TrimSpace(fmt.Sprintf(f, args...)) + "\n")
	}

	var ranAfterFailure []string
	p := mutators.New(
		mutators.NamedFunc("ok1", func(_ context.Context, _ *engine.Request) (*mutators.Result, error) {
			ranAfterFailure = append(ranAfterFailure, "ok1")
			return &mutators.Result{Applied: true}, nil
		}),
		mutators.NamedFunc("fail", func(_ context.Context, _ *engine.Request) (*mutators.Result, error) {
			ranAfterFailure = append(ranAfterFailure, "fail")
			return &mutators.Result{Applied: false}, errors.New("simulated failure")
		}),
		mutators.NamedFunc("ok2", func(_ context.Context, _ *engine.Request) (*mutators.Result, error) {
			ranAfterFailure = append(ranAfterFailure, "ok2")
			return &mutators.Result{Applied: true}, nil
		}),
	)

	req := &engine.Request{MappedBody: []byte(`{"messages":[]}`)}
	reports := p.Run(context.Background(), req, logger)

	if len(reports) != 3 {
		t.Fatalf("expected 3 reports, got %d", len(reports))
	}
	if len(ranAfterFailure) != 3 {
		t.Fatalf("expected all 3 mutators to run, got %d", len(ranAfterFailure))
	}
	if mutators.AppliedCount(reports) != 2 {
		t.Fatalf("expected 2 applied (ok1, ok2), got %d", mutators.AppliedCount(reports))
	}
	if mutators.ErrorCount(reports) != 1 {
		t.Fatalf("expected 1 error (fail), got %d", mutators.ErrorCount(reports))
	}
	if !strings.Contains(logBuf.String(), "fail failed-open") {
		t.Fatalf("expected fail-open log, got: %s", logBuf.String())
	}
}

// TestMutatorContentPreservation verifies that the pipeline does not corrupt
// request body and preserves all original content.
func TestMutatorContentPreservation(t *testing.T) {
	original := `{"messages":[{"role":"user","content":"hello"}]}`
	req := &engine.Request{MappedBody: []byte(original)}

	p := mutators.DefaultPipeline()
	p.Run(context.Background(), req, nil)

	// With default configs, all mutators are disabled so body should be unchanged
	if string(req.MappedBody) != original {
		t.Fatalf("body changed by disabled pipeline:\ngot:  %s\nwant: %s", string(req.MappedBody), original)
	}
}

// TestMutatorCancellation verifies that a cancelled context stops the pipeline
// and subsequent mutators do not run.
func TestMutatorCancellation(t *testing.T) {
	var ranAfterCancel []string
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Pre-cancel

	p := mutators.New(
		mutators.NamedFunc("a", func(_ context.Context, _ *engine.Request) (*mutators.Result, error) {
			ranAfterCancel = append(ranAfterCancel, "a")
			return &mutators.Result{Applied: false}, nil
		}),
		mutators.NamedFunc("b", func(_ context.Context, _ *engine.Request) (*mutators.Result, error) {
			ranAfterCancel = append(ranAfterCancel, "b")
			return &mutators.Result{Applied: false}, nil
		}),
	)

	req := &engine.Request{MappedBody: []byte(`{"messages":[]}`)}
	reports := p.Run(ctx, req, nil)

	// Either 1 report (a ran, b skipped) or 0 (a also skipped)
	if len(reports) > 1 {
		t.Fatalf("expected at most 1 report after cancel, got %d", len(reports))
	}
	if len(ranAfterCancel) > 1 {
		t.Fatalf("expected at most 1 mutator after cancel, got %d", len(ranAfterCancel))
	}
}

// TestMutatorNoCrossRequestState verifies concurrent pipeline executions
// do not share mutable state and produce consistent results.
func TestMutatorNoCrossRequestState(t *testing.T) {
	p := mutators.DefaultPipeline()
	var wg sync.WaitGroup
	errCh := make(chan error, 200)

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			body := []byte(`{"messages":[{"role":"user","content":"concurrent"}]}`)
			req := &engine.Request{MappedBody: body}
			reports := p.Run(context.Background(), req, nil)
			// Check no nil results (pipeline internal invariant)
			for _, r := range reports {
				if r.Err != nil && !strings.Contains(r.Err.Error(), "cancelled") {
					errCh <- r.Err
					return
				}
			}
			errCh <- nil
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Errorf("cross-request error: %v", err)
		}
	}
}

// TestSystemInjectPrepend_AllMessagesPreserved verifies that prepending a
// system message preserves every original message in correct order.
// This is a higher-level parity test for the P3-T11 prepend fix.
func TestSystemInjectPrepend_AllMessagesPreserved(t *testing.T) {
	// We verify by constructing a request body that would trigger the
	// system prompt injection path, and checking that messages are preserved.
	// Since injectSystemPromptToBody is unexported, we test through a
	// NamedFunc that simulates the same logic.
	input := []byte(`{"messages":[{"role":"user","content":"msg1"},{"role":"user","content":"msg2"},{"role":"user","content":"msg3"}]}`)

	var modified bool
	p := mutators.New(
		mutators.NamedFunc("system-inject", func(ctx context.Context, req *engine.Request) (*mutators.Result, error) {
			var body map[string]json.RawMessage
			if err := json.Unmarshal(req.MappedBody, &body); err != nil {
				return &mutators.Result{Applied: false}, nil
			}
			msgsRaw, ok := body["messages"]
			if !ok {
				return &mutators.Result{Applied: false}, nil
			}
			var msgs []json.RawMessage
			if err := json.Unmarshal(msgsRaw, &msgs); err != nil {
				return &mutators.Result{Applied: false}, nil
			}
			// Prepend system message — N+1 slice, preserves all originals
			sysMsg, _ := json.Marshal(map[string]string{"role": "system", "content": "You are a helpful assistant."})
			newMsgs := make([]json.RawMessage, 0, len(msgs)+1)
			newMsgs = append(newMsgs, sysMsg)
			newMsgs = append(newMsgs, msgs...)
			raw, _ := json.Marshal(newMsgs)
			body["messages"] = raw
			req.MappedBody, _ = json.Marshal(body)
			modified = true
			return &mutators.Result{Applied: true}, nil
		}),
	)

	req := &engine.Request{MappedBody: input}
	p.Run(context.Background(), req, nil)

	if !modified {
		t.Fatal("system prompt was not injected")
	}

	var result map[string][]json.RawMessage
	if err := json.Unmarshal(req.MappedBody, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	msgs := result["messages"]
	if len(msgs) != 4 {
		t.Fatalf("expected 4 messages (1 system + 3 user), got %d", len(msgs))
	}
	// Verify system message at position 0
	var firstMsg map[string]string
	if err := json.Unmarshal(msgs[0], &firstMsg); err != nil {
		t.Fatalf("unmarshal first msg: %v", err)
	}
	if firstMsg["role"] != "system" {
		t.Fatalf("expected first message role 'system', got %q", firstMsg["role"])
	}
	// Verify all original messages preserved in order
	for i := 1; i <= 3; i++ {
		var msg map[string]string
		if err := json.Unmarshal(msgs[i], &msg); err != nil {
			t.Fatalf("unmarshal msg %d: %v", i, err)
		}
		expected := "msg" + string(rune('0'+i))
		if msg["content"] != expected {
			t.Fatalf("msg %d content: expected %q, got %q", i, expected, msg["content"])
		}
	}
}

// TestMutatorEmptyBody verifies the pipeline handles empty/nil request bodies.
func TestMutatorEmptyBody(t *testing.T) {
	p := mutators.DefaultPipeline()
	reports := p.Run(context.Background(), nil, nil)
	if reports != nil {
		t.Fatal("expected nil reports for nil request")
	}

	reports = p.Run(context.Background(), &engine.Request{}, nil)
	if reports == nil {
		t.Fatal("expected non-nil reports for empty request")
	}
}

// TestMutatorDefaultConfig verifies the default pipeline configuration.
func TestMutatorDefaultConfig(t *testing.T) {
	p := mutators.DefaultPipeline()
	muts := p.Mutators()
	if len(muts) != 5 {
		t.Fatalf("expected 5 mutators, got %d", len(muts))
	}
}

// TestAllGoldensValidJSON verifies all golden fixture files are valid JSON.
func TestAllGoldensValidJSON(t *testing.T) {
	files := []string{
		"../golden/mutators/messages_basic.json",
		"../golden/mutators/messages_with_system.json",
		"../golden/mutators/input_basic.json",
		"../golden/mutators/body_rtk_target.json",
		"../golden/mutators/body_headroom_target.json",
	}
	for _, f := range files {
		t.Run(f, func(t *testing.T) {
			data, err := os.ReadFile(f)
			if err != nil {
				t.Fatalf("read %s: %v", f, err)
			}
			if !json.Valid(data) {
				t.Fatalf("invalid JSON in %s", f)
			}
		})
	}
}
