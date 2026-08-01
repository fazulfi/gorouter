package combos

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"gorouter/internal/domain/combo"
	enginecombos "gorouter/internal/engine/combos"

	"github.com/google/uuid"
)

type fakeRepo struct {
	mu       sync.Mutex
	defs     map[uuid.UUID]combo.Definition
	byName   map[string]uuid.UUID
	members  map[uuid.UUID][]combo.Member
	state    map[string]json.RawMessage
	stateTTL map[string]time.Time
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		defs:     map[uuid.UUID]combo.Definition{},
		byName:   map[string]uuid.UUID{},
		members:  map[uuid.UUID][]combo.Member{},
		state:    map[string]json.RawMessage{},
		stateTTL: map[string]time.Time{},
	}
}

func (f *fakeRepo) Create(ctx context.Context, d *combo.Definition) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.defs[d.ID] = *d
	f.byName[d.Name] = d.ID
	return nil
}

func (f *fakeRepo) FindByID(ctx context.Context, id uuid.UUID) (*combo.Definition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.defs[id]
	if !ok {
		return nil, nil
	}
	cp := d
	return &cp, nil
}

func (f *fakeRepo) FindByName(ctx context.Context, name string) (*combo.Definition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, ok := f.byName[name]
	if !ok {
		return nil, nil
	}
	d, ok := f.defs[id]
	if !ok {
		return nil, nil
	}
	cp := d
	return &cp, nil
}

func (f *fakeRepo) List(ctx context.Context) ([]combo.Definition, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []combo.Definition{}
	for _, d := range f.defs {
		out = append(out, d)
	}
	return out, nil
}

func (f *fakeRepo) Update(ctx context.Context, d *combo.Definition) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.defs[d.ID]; !ok {
		return combo.ErrNotFound
	}
	delete(f.byName, f.defs[d.ID].Name)
	f.defs[d.ID] = *d
	f.byName[d.Name] = d.ID
	return nil
}

func (f *fakeRepo) Delete(ctx context.Context, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if d, ok := f.defs[id]; ok {
		delete(f.byName, d.Name)
	}
	delete(f.defs, id)
	delete(f.members, id)
	return nil
}

func (f *fakeRepo) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	d, ok := f.defs[id]
	if !ok {
		return combo.ErrNotFound
	}
	d.IsActive = active
	f.defs[id] = d
	return nil
}

func (f *fakeRepo) ListMembers(ctx context.Context, comboID uuid.UUID) ([]combo.Member, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]combo.Member, len(f.members[comboID]))
	copy(out, f.members[comboID])
	return out, nil
}

func (f *fakeRepo) UpsertMembers(ctx context.Context, comboID uuid.UUID, members []combo.Member) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.members[comboID] = members
	return nil
}

func (f *fakeRepo) DeleteMember(ctx context.Context, memberID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for id, members := range f.members {
		out := members[:0]
		for _, m := range members {
			if m.ID != memberID {
				out = append(out, m)
			}
		}
		f.members[id] = out
	}
	return nil
}

func (f *fakeRepo) Get(ctx context.Context, key string) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	raw, ok := f.state[key]
	if !ok {
		return nil, nil
	}
	if ttl, has := f.stateTTL[key]; has && time.Now().After(ttl) {
		delete(f.state, key)
		return nil, nil
	}
	return raw, nil
}

func (f *fakeRepo) Set(ctx context.Context, key string, value json.RawMessage, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.state[key] = value
	if ttl > 0 {
		f.stateTTL[key] = time.Now().Add(ttl)
	}
	return nil
}

func testMembers(n int) []combo.Member {
	out := make([]combo.Member, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, combo.Member{
			ID:         uuid.New(),
			ProviderID: uuid.New(),
			ModelRef:   "provider/model-" + string(rune('a'+i)),
			Priority:   i,
			Weight:     1,
			IsActive:   true,
		})
	}
	return out
}

func newTestService(repo *fakeRepo) (*Service, *[]invocation, *[]string) {
	var mu sync.Mutex
	var calls []invocation
	var judgeResolutions []string
	invoke := func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
		mu.Lock()
		calls = append(calls, invocation{member: m, request: string(req)})
		mu.Unlock()
		return []byte("resp:" + m.ModelRef), nil
	}
	checkCaps := func(ctx context.Context, m combo.Member, caps []string) (bool, error) {
		return true, nil
	}
	resolveJudge := func(ctx context.Context, judgeModelID string) (*combo.Member, error) {
		judgeResolutions = append(judgeResolutions, judgeModelID)
		if judgeModelID == "unknown/model" {
			return nil, errors.New("model not configured")
		}
		return &combo.Member{
			ID:         uuid.New(),
			ProviderID: uuid.New(),
			ModelRef:   judgeModelID,
			IsActive:   true,
		}, nil
	}
	return NewService(repo, repo, invoke, checkCaps, resolveJudge), &calls, &judgeResolutions
}

type invocation struct {
	member  combo.Member
	request string
}

func TestService_CreatePersists(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _ := newTestService(repo)
	ctx := context.Background()

	def, err := svc.Create(ctx, "my-combo", combo.StrategySequential, nil, testMembers(2))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if def.ID == uuid.Nil || !def.IsActive || def.Name != "my-combo" {
		t.Errorf("definition = %+v", def)
	}
	got, err := svc.Get(ctx, def.ID)
	if err != nil || got == nil || got.ID != def.ID {
		t.Errorf("Get = %+v, %v", got, err)
	}
	members, err := repo.ListMembers(ctx, def.ID)
	if err != nil || len(members) != 2 {
		t.Errorf("members = %d, %v", len(members), err)
	}
	for _, m := range members {
		if m.ComboID != def.ID || m.CreatedAt.IsZero() {
			t.Errorf("member not stamped: %+v", m)
		}
	}
}

func TestService_CreateValidation(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _ := newTestService(repo)
	ctx := context.Background()

	cases := []struct {
		name     string
		strategy combo.Strategy
		config   json.RawMessage
		members  []combo.Member
	}{
		{"", combo.StrategySequential, nil, testMembers(1)},
		{"x", combo.Strategy("bogus"), nil, testMembers(1)},
		{"x", combo.StrategyFusion, json.RawMessage(`{}`), testMembers(1)},
		{"x", combo.StrategySequential, nil, nil},
		{"x", combo.StrategySequential, nil, []combo.Member{{ID: uuid.New(), IsActive: true}}},
	}
	for i, c := range cases {
		_, err := svc.Create(ctx, c.name, c.strategy, c.config, c.members)
		if err == nil {
			t.Errorf("case %d: expected validation error", i)
		}
	}
	if len(repo.defs) != 0 {
		t.Errorf("invalid creates must not persist, got %d defs", len(repo.defs))
	}
}

func TestService_UpdateDeleteSetActive(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _ := newTestService(repo)
	ctx := context.Background()

	def, err := svc.Create(ctx, "c", combo.StrategyRoundRobin, json.RawMessage(`{"sticky_count":3}`), testMembers(2))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := svc.SetActive(ctx, def.ID, false); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	got, _ := svc.Get(ctx, def.ID)
	if got.IsActive {
		t.Error("expected inactive after SetActive(false)")
	}
	def.IsActive = true
	def.Name = "renamed"
	if err := svc.Update(ctx, def); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ = svc.Get(ctx, def.ID)
	if got.Name != "renamed" {
		t.Errorf("name = %s", got.Name)
	}
	if err := svc.Delete(ctx, def.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(ctx, def.ID); !errors.Is(err, combo.ErrNotFound) {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
}

func TestService_ExecuteSequentialFallsBack(t *testing.T) {
	repo := newFakeRepo()
	svc, calls, _ := newTestService(repo)
	ctx := context.Background()
	def, _ := svc.Create(ctx, "seq", combo.StrategySequential, nil, testMembers(2))
	*calls = (*calls)[:0]
	failing := 0
	invoke := func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
		*calls = append(*calls, invocation{member: m, request: string(req)})
		if failing == 0 {
			failing++
			return nil, errors.New("transient")
		}
		return []byte("ok"), nil
	}
	svc.invoke = invoke
	res, err := svc.Execute(ctx, def.ID, []byte(`{"model":"seq"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*calls) != 2 {
		t.Errorf("calls = %d, want 2 (fallback)", len(*calls))
	}
	if res.Member.ModelRef != (*calls)[1].member.ModelRef {
		t.Errorf("result member mismatch")
	}
}

func TestService_ExecuteInactiveAndMissing(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _ := newTestService(repo)
	ctx := context.Background()

	_, err := svc.Execute(ctx, uuid.New(), nil, nil)
	if !errors.Is(err, combo.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	def, _ := svc.Create(ctx, "off", combo.StrategySequential, nil, testMembers(1))
	_ = svc.SetActive(ctx, def.ID, false)
	_, err = svc.Execute(ctx, def.ID, nil, nil)
	if !errors.Is(err, combo.ErrInactive) {
		t.Errorf("err = %v, want ErrInactive", err)
	}
}

func TestService_ExecuteRoundRobinPersistsRotation(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _ := newTestService(repo)
	ctx := context.Background()
	def, _ := svc.Create(ctx, "rr", combo.StrategyRoundRobin, json.RawMessage(`{"sticky_count":1}`), testMembers(2))

	first, err := svc.Execute(ctx, def.ID, []byte("req"), nil)
	if err != nil {
		t.Fatalf("Execute 1: %v", err)
	}
	second, err := svc.Execute(ctx, def.ID, []byte("req"), nil)
	if err != nil {
		t.Fatalf("Execute 2: %v", err)
	}
	if first.Member.ID == second.Member.ID {
		t.Error("round robin must rotate members")
	}

	// A restarted service (fresh engines, same repo state) continues rotation.
	svc2, _, _ := newTestService(repo)
	third, err := svc2.Execute(ctx, def.ID, []byte("req"), nil)
	if err != nil {
		t.Fatalf("Execute 3: %v", err)
	}
	if third.Member.ID != first.Member.ID {
		t.Errorf("rotation must continue after restart, got %s want %s", third.Member.ModelRef, first.Member.ModelRef)
	}
}

func TestService_ExecuteRoundRobinSticky(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _ := newTestService(repo)
	ctx := context.Background()
	def, _ := svc.Create(ctx, "rr2", combo.StrategyRoundRobin, json.RawMessage(`{"sticky_count":2}`), testMembers(2))

	one, _ := svc.Execute(ctx, def.ID, []byte("req"), nil)
	two, _ := svc.Execute(ctx, def.ID, []byte("req"), nil)
	three, _ := svc.Execute(ctx, def.ID, []byte("req"), nil)
	if one.Member.ID != two.Member.ID {
		t.Error("sticky: second request must reuse first member")
	}
	if three.Member.ID == one.Member.ID {
		t.Error("sticky: third request must rotate")
	}
}

func TestService_ExecuteAutoSwitch(t *testing.T) {
	repo := newFakeRepo()
	svc, calls, _ := newTestService(repo)
	ctx := context.Background()
	def, _ := svc.Create(ctx, "as", combo.StrategyAutoSwitch, nil, testMembers(2))

	svc.checkCaps = func(ctx context.Context, m combo.Member, caps []string) (bool, error) {
		return m.ModelRef == "provider/model-b", nil
	}
	_, err := svc.Execute(ctx, def.ID, []byte("req"), []string{"images"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(*calls) != 1 || (*calls)[0].member.ModelRef != "provider/model-b" {
		t.Errorf("auto-switch should pick compatible member, calls = %+v", calls)
	}
}

func TestService_ExecuteFusionResolvesJudgeFirst(t *testing.T) {
	repo := newFakeRepo()
	svc, calls, resolutions := newTestService(repo)
	ctx := context.Background()
	def, _ := svc.Create(ctx, "fus", combo.StrategyFusion,
		json.RawMessage(`{"quorum":2,"grace_period":"30ms","hard_timeout":"500ms","judge_model_id":"unknown/model"}`),
		testMembers(2))

	_, err := svc.Execute(ctx, def.ID, []byte("req"), nil)
	if !errors.Is(err, combo.ErrJudgeModelUnresolved) {
		t.Fatalf("err = %v, want ErrJudgeModelUnresolved", err)
	}
	if len(*resolutions) != 1 || (*resolutions)[0] != "unknown/model" {
		t.Errorf("resolutions = %v", resolutions)
	}
	if len(*calls) != 0 {
		t.Errorf("no panel may run when judge model is unresolved, calls = %d", len(*calls))
	}
}

func TestService_ExecuteFusionJudgeSynthesis(t *testing.T) {
	repo := newFakeRepo()
	svc, calls, _ := newTestService(repo)
	ctx := context.Background()
	def, _ := svc.Create(ctx, "fus2", combo.StrategyFusion,
		json.RawMessage(`{"quorum":2,"grace_period":"200ms","hard_timeout":"2s","judge_model_id":"judge/model"}`),
		testMembers(2))

	res, err := svc.Execute(ctx, def.ID, []byte("req"), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.JudgeUsed {
		t.Error("expected judge synthesis with quorum of successes")
	}
	if len(*calls) != 3 {
		t.Fatalf("calls = %d, want 2 panels + 1 judge", len(*calls))
	}
	judgeCall := (*calls)[2]
	if judgeCall.member.ModelRef != "judge/model" {
		t.Errorf("judge call member = %s", judgeCall.member.ModelRef)
	}
	var body map[string]interface{}
	if err := json.Unmarshal([]byte(judgeCall.request), &body); err != nil {
		t.Fatalf("judge body not JSON: %v", err)
	}
	if body["judge_model_id"] != "judge/model" {
		t.Errorf("judge body = %v", body)
	}
	panels, ok := body["panels"].([]interface{})
	if !ok || len(panels) != 2 {
		t.Errorf("judge panels = %v", body["panels"])
	}
}

func TestService_ExecuteFusionNoPanels(t *testing.T) {
	repo := newFakeRepo()
	svc, _, _ := newTestService(repo)
	ctx := context.Background()
	def, _ := svc.Create(ctx, "fus3", combo.StrategyFusion,
		json.RawMessage(`{"quorum":2,"grace_period":"30ms","hard_timeout":"500ms","judge_model_id":"judge/model"}`),
		[]combo.Member{{ID: uuid.New(), ProviderID: uuid.New(), ModelRef: "x", IsActive: false}})

	_, err := svc.Execute(ctx, def.ID, []byte("req"), nil)
	if !errors.Is(err, enginecombos.ErrNoEligibleMember) {
		t.Errorf("err = %v, want ErrNoEligibleMember", err)
	}
}
