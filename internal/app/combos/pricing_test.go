package combos

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/pricing"

	"github.com/google/uuid"
)

// fakePricingRepo is the in-memory backing store for the fake scope.
type fakePricingRepo struct {
	mu        sync.Mutex
	overrides []pricing.PriceOverride
	loadErr   error
	saveErr   error
}

func newFakePricingRepo() *fakePricingRepo { return &fakePricingRepo{} }

func (f *fakePricingRepo) LoadOverrides(_ context.Context) ([]pricing.PriceOverride, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return append([]pricing.PriceOverride(nil), f.overrides...), nil
}

func (f *fakePricingRepo) SaveOverrides(_ context.Context, overrides []pricing.PriceOverride) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	f.overrides = append([]pricing.PriceOverride(nil), overrides...)
	return nil
}

// fakePricingAuditLog records audit entries immutably; details bytes are
// copied so later entries can never alias earlier ones.
type fakePricingAuditLog struct {
	mu      sync.Mutex
	entries []tx.AuditLogEntry
	err     error
}

func (f *fakePricingAuditLog) Create(_ context.Context, entry *tx.AuditLogEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	cp := *entry
	cp.Details = append(json.RawMessage(nil), entry.Details...)
	f.entries = append(f.entries, cp)
	return nil
}

// fakePricingScope models the transaction contract: repository writes are
// staged and only become visible in the backing store on Commit; Rollback
// discards them.
type fakePricingScope struct {
	backing   *fakePricingRepo
	audit     *fakePricingAuditLog
	mu        sync.Mutex
	pending   []pricing.PriceOverride
	dirty     bool
	commits   int
	rollbacks int
	commitErr error
}

func newFakePricingScope() *fakePricingScope {
	return &fakePricingScope{backing: newFakePricingRepo(), audit: &fakePricingAuditLog{}}
}

type stagedPricingRepo struct{ scope *fakePricingScope }

func (r *stagedPricingRepo) LoadOverrides(ctx context.Context) ([]pricing.PriceOverride, error) {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	if r.scope.dirty {
		return append([]pricing.PriceOverride(nil), r.scope.pending...), nil
	}
	return r.scope.backing.LoadOverrides(ctx)
}

func (r *stagedPricingRepo) SaveOverrides(_ context.Context, overrides []pricing.PriceOverride) error {
	r.scope.mu.Lock()
	defer r.scope.mu.Unlock()
	r.scope.pending = append([]pricing.PriceOverride(nil), overrides...)
	r.scope.dirty = true
	return nil
}

func (s *fakePricingScope) Pricing() pricing.PricingRepository { return &stagedPricingRepo{scope: s} }
func (s *fakePricingScope) AuditLog() tx.AuditLogRepository    { return s.audit }

func (s *fakePricingScope) Commit(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.commitErr != nil {
		return s.commitErr
	}
	s.commits++
	if s.dirty {
		s.backing.mu.Lock()
		s.backing.overrides = append([]pricing.PriceOverride(nil), s.pending...)
		s.backing.mu.Unlock()
		s.pending = nil
		s.dirty = false
	}
	return nil
}

func (s *fakePricingScope) Rollback(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rollbacks++
	s.pending = nil
	s.dirty = false
	return nil
}

type fakePricingBeginner struct {
	scope *fakePricingScope
	err   error
}

func (f *fakePricingBeginner) Begin(_ context.Context) (PricingScope, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.scope, nil
}

func newTestPricingService() (*PricingService, *fakePricingScope) {
	scope := newFakePricingScope()
	return NewPricingService(&fakePricingBeginner{scope: scope}), scope
}

func testPricingActor() *auth.Actor {
	return &auth.Actor{UserID: uuid.New(), IsAdmin: true}
}

func testPriceOverride(providerID, modelID string, inputPrice, outputPrice float64) pricing.PriceOverride {
	return pricing.PriceOverride{
		ModelID:     modelID,
		ProviderID:  providerID,
		InputPrice:  inputPrice,
		OutputPrice: outputPrice,
	}
}

func decodeDetails(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode audit details: %v", err)
	}
	return m
}

// TestPricingOverrideAuditTrail is the atom's one-behavior test: an override
// write produces an append-only audit row with a sanitized before/after diff,
// and history is immutable (no update path).
func TestPricingOverrideAuditTrail(t *testing.T) {
	svc, scope := newTestPricingService()
	ctx := context.Background()
	actor := testPricingActor()

	// The audit surface is append-only by contract: the repository interface
	// exposes exactly one method (Create); there is no update or delete path.
	repoType := reflect.TypeOf((*tx.AuditLogRepository)(nil)).Elem()
	if repoType.NumMethod() != 1 || repoType.Method(0).Name != "Create" {
		t.Fatalf("audit repository must be append-only (single Create method), got %d methods",
			repoType.NumMethod())
	}

	first, err := svc.ApplyOverride(ctx, actor, testPriceOverride("openai", "gpt-4o", 1.0, 2.0))
	if err != nil {
		t.Fatalf("ApplyOverride (create): %v", err)
	}
	if scope.commits != 1 {
		t.Errorf("commits = %d, want 1", scope.commits)
	}
	if first.UpdatedBy != actor.UserID || first.UpdatedAt.IsZero() {
		t.Errorf("applied override not stamped: %+v", first)
	}

	if n := len(scope.audit.entries); n != 1 {
		t.Fatalf("audit entries = %d, want 1", n)
	}
	firstEntry := scope.audit.entries[0]
	if firstEntry.ActorID == nil || *firstEntry.ActorID != actor.UserID {
		t.Errorf("audit actor = %v, want %v", firstEntry.ActorID, actor.UserID)
	}
	if firstEntry.Action != "pricing.override.apply" || firstEntry.ResourceType != "pricing_override" {
		t.Errorf("audit action/resource = %q/%q", firstEntry.Action, firstEntry.ResourceType)
	}
	det := decodeDetails(t, firstEntry.Details)
	if det["result"] != "applied" {
		t.Errorf("audit result = %v, want applied", det["result"])
	}
	target, ok := det["target"].(map[string]any)
	if !ok || target["provider_id"] != "openai" || target["model_id"] != "gpt-4o" {
		t.Errorf("audit target = %v", det["target"])
	}
	if _, ok := det["before"]; ok {
		t.Errorf("creation audit must not carry a before diff: %v", det)
	}
	after, ok := det["after"].(map[string]any)
	if !ok || after["input_price"] != 1.0 || after["output_price"] != 2.0 {
		t.Errorf("audit after diff = %v", det["after"])
	}

	// Replacement appends a second row carrying the sanitized before/after
	// diff; the first row is never mutated.
	if _, err := svc.ApplyOverride(ctx, actor, testPriceOverride("openai", "gpt-4o", 3.5, 4.5)); err != nil {
		t.Fatalf("ApplyOverride (replace): %v", err)
	}
	if n := len(scope.audit.entries); n != 2 {
		t.Fatalf("audit entries = %d, want 2 (immutable history)", n)
	}
	if string(scope.audit.entries[0].Details) != string(firstEntry.Details) {
		t.Errorf("first audit row was mutated: %q -> %q", firstEntry.Details, scope.audit.entries[0].Details)
	}
	det2 := decodeDetails(t, scope.audit.entries[1].Details)
	before, ok := det2["before"].(map[string]any)
	if !ok || before["input_price"] != 1.0 || before["output_price"] != 2.0 {
		t.Errorf("replacement before diff = %v", det2["before"])
	}
	after2, ok := det2["after"].(map[string]any)
	if !ok || after2["input_price"] != 3.5 || after2["output_price"] != 4.5 {
		t.Errorf("replacement after diff = %v", det2["after"])
	}

	list, err := svc.ListOverrides(ctx)
	if err != nil {
		t.Fatalf("ListOverrides: %v", err)
	}
	if len(list) != 1 || list[0].InputPrice != 3.5 {
		t.Errorf("materialized overrides = %+v, want single replacement", list)
	}
}

func TestPricingService_PreservesCallerInput(t *testing.T) {
	svc, scope := newTestPricingService()
	ctx := context.Background()
	actor := testPricingActor()
	before := testPriceOverride("anthropic", "claude-3-5-sonnet", 3.0, 15.0)

	applied, err := svc.ApplyOverride(ctx, actor, before)
	if err != nil {
		t.Fatalf("ApplyOverride: %v", err)
	}
	if before.UpdatedBy != uuid.Nil || !before.UpdatedAt.IsZero() {
		t.Errorf("caller input mutated: %+v", before)
	}
	if applied.UpdatedBy != actor.UserID || applied.UpdatedAt.IsZero() {
		t.Errorf("applied copy not stamped: %+v", applied)
	}
	backing, err := scope.backing.LoadOverrides(ctx)
	if err != nil {
		t.Fatalf("backing load: %v", err)
	}
	if len(backing) != 1 || backing[0].UpdatedBy != actor.UserID {
		t.Errorf("backing store = %+v", backing)
	}
}

func TestPricingService_ReplacementKeepsOtherOverrides(t *testing.T) {
	svc, _ := newTestPricingService()
	ctx := context.Background()
	actor := testPricingActor()

	if _, err := svc.ApplyOverride(ctx, actor, testPriceOverride("openai", "gpt-4o", 1.0, 2.0)); err != nil {
		t.Fatalf("apply gpt-4o: %v", err)
	}
	if _, err := svc.ApplyOverride(ctx, actor, testPriceOverride("openai", "gpt-4o-mini", 0.15, 0.6)); err != nil {
		t.Fatalf("apply gpt-4o-mini: %v", err)
	}
	if _, err := svc.ApplyOverride(ctx, actor, testPriceOverride("openai", "gpt-4o", 2.5, 10.0)); err != nil {
		t.Fatalf("replace gpt-4o: %v", err)
	}

	list, err := svc.ListOverrides(ctx)
	if err != nil {
		t.Fatalf("ListOverrides: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("overrides = %d, want 2", len(list))
	}
	byKey := map[string]pricing.PriceOverride{}
	for _, o := range list {
		byKey[o.Key()] = o
	}
	if got := byKey["openai/gpt-4o"]; got.InputPrice != 2.5 {
		t.Errorf("gpt-4o price = %v, want 2.5", got.InputPrice)
	}
	if got := byKey["openai/gpt-4o-mini"]; got.InputPrice != 0.15 {
		t.Errorf("gpt-4o-mini price = %v, want 0.15", got.InputPrice)
	}
}

func TestPricingService_ListOverridesDeterministic(t *testing.T) {
	svc, _ := newTestPricingService()
	ctx := context.Background()
	actor := testPricingActor()

	for _, o := range []pricing.PriceOverride{
		testPriceOverride("b-provider", "model-2", 1, 1),
		testPriceOverride("a-provider", "model-1", 1, 1),
		testPriceOverride("a-provider", "model-0", 1, 1),
	} {
		if _, err := svc.ApplyOverride(ctx, actor, o); err != nil {
			t.Fatalf("apply %s: %v", o.Key(), err)
		}
	}
	list, err := svc.ListOverrides(ctx)
	if err != nil {
		t.Fatalf("ListOverrides: %v", err)
	}
	want := []string{"a-provider/model-0", "a-provider/model-1", "b-provider/model-2"}
	if len(list) != len(want) {
		t.Fatalf("list = %d entries, want %d", len(list), len(want))
	}
	for i, w := range want {
		if list[i].Key() != w {
			t.Errorf("position %d = %s, want %s", i, list[i].Key(), w)
		}
	}
}

func TestPricingService_ValidationErrors(t *testing.T) {
	svc, scope := newTestPricingService()
	ctx := context.Background()
	actor := testPricingActor()

	cases := []struct {
		name     string
		override pricing.PriceOverride
	}{
		{"missing model", testPriceOverride("openai", "", 1, 2)},
		{"whitespace model", testPriceOverride("openai", "   ", 1, 2)},
		{"missing provider", testPriceOverride("", "gpt-4o", 1, 2)},
		{"negative input price", testPriceOverride("openai", "gpt-4o", -1, 2)},
		{"negative output price", testPriceOverride("openai", "gpt-4o", 1, -2)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := svc.ApplyOverride(ctx, actor, c.override); err == nil {
				t.Fatal("ApplyOverride = nil error, want validation error")
			}
		})
	}
	if scope.commits != 0 {
		t.Errorf("commits = %d, want 0 for invalid input", scope.commits)
	}
	if len(scope.audit.entries) != 0 {
		t.Errorf("audit entries = %d, want 0 for invalid input", len(scope.audit.entries))
	}
	backing, err := scope.backing.LoadOverrides(ctx)
	if err != nil {
		t.Fatalf("backing load: %v", err)
	}
	if len(backing) != 0 {
		t.Errorf("backing store mutated by invalid input: %+v", backing)
	}
}

func TestPricingService_RequiresActor(t *testing.T) {
	svc, scope := newTestPricingService()
	ctx := context.Background()

	if _, err := svc.ApplyOverride(ctx, nil, testPriceOverride("openai", "gpt-4o", 1, 2)); err == nil {
		t.Fatal("ApplyOverride with nil actor = nil error, want error")
	}
	if scope.commits != 0 || len(scope.audit.entries) != 0 {
		t.Errorf("nil actor must not write anything: commits=%d audits=%d", scope.commits, len(scope.audit.entries))
	}
}

func TestPricingService_RollbackOnAuditFailure(t *testing.T) {
	svc, scope := newTestPricingService()
	ctx := context.Background()
	actor := testPricingActor()
	scope.audit.err = errors.New("audit log unavailable")

	if _, err := svc.ApplyOverride(ctx, actor, testPriceOverride("openai", "gpt-4o", 1.0, 2.0)); err == nil {
		t.Fatal("ApplyOverride = nil error, want audit failure")
	}
	if scope.commits != 0 {
		t.Errorf("commits = %d, want 0 (transaction must not commit)", scope.commits)
	}
	if scope.rollbacks == 0 {
		t.Error("rollbacks = 0, want deferred rollback")
	}
	backing, err := scope.backing.LoadOverrides(ctx)
	if err != nil {
		t.Fatalf("backing load: %v", err)
	}
	if len(backing) != 0 {
		t.Errorf("settings write leaked despite audit failure: %+v", backing)
	}
}

func TestPricingService_CommitFailure(t *testing.T) {
	svc, scope := newTestPricingService()
	ctx := context.Background()
	actor := testPricingActor()
	scope.commitErr = errors.New("commit failed")

	if _, err := svc.ApplyOverride(ctx, actor, testPriceOverride("openai", "gpt-4o", 1.0, 2.0)); err == nil {
		t.Fatal("ApplyOverride = nil error, want commit failure")
	}
	if scope.rollbacks == 0 {
		t.Error("rollbacks = 0, want deferred rollback after commit failure")
	}
	backing, err := scope.backing.LoadOverrides(ctx)
	if err != nil {
		t.Fatalf("backing load: %v", err)
	}
	if len(backing) != 0 {
		t.Errorf("settings write leaked despite commit failure: %+v", backing)
	}
}

func TestPricingService_BeginFailure(t *testing.T) {
	svc, scope := newTestPricingService()
	beginner := &fakePricingBeginner{scope: scope, err: errors.New("no connection")}
	svc.beginner = beginner
	ctx := context.Background()

	if _, err := svc.ApplyOverride(ctx, testPricingActor(), testPriceOverride("openai", "gpt-4o", 1, 2)); err == nil {
		t.Fatal("ApplyOverride = nil error, want begin failure")
	}
	if _, err := svc.ListOverrides(ctx); err == nil {
		t.Fatal("ListOverrides = nil error, want begin failure")
	}
}
