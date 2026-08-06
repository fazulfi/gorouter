package providers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

var (
	errAuditWrite = errors.New("audit write failed")
	errRepo       = errors.New("repo failed")
)

func testLogger() zerolog.Logger { return zerolog.Nop() }

func TestValidate(t *testing.T) {
	ctx := context.Background()
	valid := provider.Provider{
		ID:   uuid.New(),
		Name: "openai-prod",
		Type: provider.ProviderOpenAI,
	}

	t.Run("valid provider passes", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewProviderService(b, &countingProber{}, testLogger())
		mustBegin(t, b)
		b.providers.byID[valid.ID] = valid

		if err := svc.Validate(ctx, valid.ID); err != nil {
			t.Fatalf("valid provider rejected: %v", err)
		}
	})

	t.Run("missing provider fails", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewProviderService(b, &countingProber{}, testLogger())
		if err := svc.Validate(ctx, uuid.New()); err == nil {
			t.Error("missing provider must fail")
		}
	})

	t.Run("empty name fails", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewProviderService(b, &countingProber{}, testLogger())
		mustBegin(t, b)
		p := valid
		p.Name = ""
		b.providers.byID[p.ID] = p
		if err := svc.Validate(ctx, p.ID); err == nil {
			t.Error("empty name must fail")
		}
	})

	t.Run("unknown type fails", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewProviderService(b, &countingProber{}, testLogger())
		mustBegin(t, b)
		p := valid
		p.Type = "alien"
		b.providers.byID[p.ID] = p
		if err := svc.Validate(ctx, p.ID); err == nil {
			t.Error("unknown type must fail")
		}
	})

	t.Run("invalid config json fails", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewProviderService(b, &countingProber{}, testLogger())
		mustBegin(t, b)
		p := valid
		p.Config = json.RawMessage(`{"broken":`)
		b.providers.byID[p.ID] = p
		if err := svc.Validate(ctx, p.ID); err == nil {
			t.Error("invalid config json must fail")
		}
	})

	t.Run("invalid base url fails", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewProviderService(b, &countingProber{}, testLogger())
		mustBegin(t, b)
		p := valid
		p.BaseURL = "not a url"
		b.providers.byID[p.ID] = p
		if err := svc.Validate(ctx, p.ID); err == nil {
			t.Error("invalid base url must fail")
		}
	})
}

func TestBatchBoundedConcurrency(t *testing.T) {
	ctx := context.Background()
	ids := make([]uuid.UUID, 12)
	for i := range ids {
		ids[i] = uuid.New()
	}

	seed := func(b *fakeBeginner) {
		mustBegin(t, b)
		for _, id := range ids {
			b.providers.byID[id] = provider.Provider{ID: id, Name: "prov-" + id.String()[:8], Type: provider.ProviderOpenAI}
			b.accounts.byProv[id] = []provider.Account{{ID: uuid.New(), ProviderID: id, AuthType: "api_key", IsEnabled: true}}
		}
	}

	t.Run("at most N concurrent probes and every test audited", func(t *testing.T) {
		b := newFakeBeginner()
		prober := &countingProber{sleep: 30 * time.Millisecond}
		seed(b)
		svc := NewProviderService(b, prober, testLogger())

		res, err := svc.TestBatch(ctx, ids, 4)
		if err != nil {
			t.Fatal(err)
		}
		if prober.max.Load() > 4 {
			t.Errorf("max concurrent probes = %d, want <= 4", prober.max.Load())
		}
		if prober.max.Load() < 2 {
			t.Errorf("batch did not run concurrently, max = %d", prober.max.Load())
		}
		if prober.calls.Load() != int64(len(ids)) {
			t.Errorf("probes = %d, want %d", prober.calls.Load(), len(ids))
		}
		if len(res.Results) != len(ids) {
			t.Fatalf("results = %d, want %d", len(res.Results), len(ids))
		}
		for _, r := range res.Results {
			if !r.OK {
				t.Errorf("probe %s failed: %s", r.ProviderID, r.Error)
			}
		}
		audits := collectAudits(b)
		if len(audits) != len(ids) {
			t.Fatalf("audit entries = %d, want %d", len(audits), len(ids))
		}
		byID := make(map[uuid.UUID]int)
		for _, e := range audits {
			if e.Action != "provider.test_batch" || e.ResourceType != "provider" {
				t.Errorf("unexpected audit entry: %s %s", e.Action, e.ResourceType)
			}
			byID[*e.ResourceID]++
			details := decodeDetails(t, e)
			if details["ok"] != true {
				t.Errorf("audit details ok = %v", details["ok"])
			}
		}
		for _, id := range ids {
			if byID[id] != 1 {
				t.Errorf("provider %s audited %d times, want 1", id, byID[id])
			}
		}
		if res.AuditFailures != 0 {
			t.Errorf("audit failures = %d, want 0", res.AuditFailures)
		}
	})

	t.Run("non-positive concurrency defaults to 4", func(t *testing.T) {
		b := newFakeBeginner()
		prober := &countingProber{sleep: 30 * time.Millisecond}
		seed(b)
		svc := NewProviderService(b, prober, testLogger())

		if _, err := svc.TestBatch(ctx, ids, 0); err != nil {
			t.Fatal(err)
		}
		if prober.max.Load() > 4 {
			t.Errorf("max concurrent probes = %d, want <= 4", prober.max.Load())
		}
		if prober.max.Load() < 2 {
			t.Errorf("batch did not run concurrently, max = %d", prober.max.Load())
		}
	})

	t.Run("caller ids slice is preserved", func(t *testing.T) {
		b := newFakeBeginner()
		prober := &countingProber{}
		seed(b)
		svc := NewProviderService(b, prober, testLogger())
		before := append([]uuid.UUID(nil), ids...)

		if _, err := svc.TestBatch(ctx, ids, 2); err != nil {
			t.Fatal(err)
		}
		for i := range ids {
			if ids[i] != before[i] {
				t.Fatalf("caller ids mutated at %d", i)
			}
		}
	})

	t.Run("probe failure still audits", func(t *testing.T) {
		b := newFakeBeginner()
		prober := &countingProber{err: errors.New("connection refused")}
		seed(b)
		svc := NewProviderService(b, prober, testLogger())

		res, err := svc.TestBatch(ctx, ids[:2], 4)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res.Results {
			if r.OK {
				t.Error("failed probe must not be OK")
			}
		}
		audits := collectAudits(b)
		if len(audits) != 2 {
			t.Fatalf("failed probes must still audit, got %d entries", len(audits))
		}
	})

	t.Run("audit failure does not fail probes", func(t *testing.T) {
		b := newFakeBeginner()
		b.audit.err = errAuditWrite
		prober := &countingProber{}
		seed(b)
		svc := NewProviderService(b, prober, testLogger())

		res, err := svc.TestBatch(ctx, ids[:4], 4)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res.Results {
			if !r.OK {
				t.Errorf("audit failure must not fail probe %s: %s", r.ProviderID, r.Error)
			}
		}
		if res.AuditFailures != 4 {
			t.Errorf("audit failures = %d, want 4", res.AuditFailures)
		}
		if prober.calls.Load() != 4 {
			t.Errorf("probes = %d, want 4", prober.calls.Load())
		}
	})

	t.Run("missing provider reports per-result error", func(t *testing.T) {
		b := newFakeBeginner()
		svc := NewProviderService(b, &countingProber{}, testLogger())
		missing := uuid.New()

		res, err := svc.TestBatch(ctx, []uuid.UUID{missing}, 4)
		if err != nil {
			t.Fatal(err)
		}
		if res.Results[0].OK || res.Results[0].Error == "" {
			t.Errorf("missing provider must report a per-result error: %+v", res.Results[0])
		}
		audits := collectAudits(b)
		if len(audits) != 1 {
			t.Fatalf("missing provider must still audit, got %d", len(audits))
		}
	})
}

func collectAudits(b *fakeBeginner) []*tx.AuditLogEntry {
	return b.audit.all()
}
