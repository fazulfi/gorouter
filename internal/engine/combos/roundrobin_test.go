package combos

import (
	"context"
	"errors"
	"sync"
	"testing"

	"gorouter/internal/domain/combo"

	"github.com/google/uuid"
)

type memStore struct {
	mu  sync.Mutex
	rot map[uuid.UUID]Rotation
}

func newMemStore() *memStore { return &memStore{rot: map[uuid.UUID]Rotation{}} }

func (s *memStore) GetRotation(ctx context.Context, comboID uuid.UUID) (*Rotation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.rot[comboID]
	if !ok {
		return nil, nil
	}
	cp := r
	return &cp, nil
}

func (s *memStore) SaveRotation(ctx context.Context, comboID uuid.UUID, r Rotation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rot[comboID] = r
	return nil
}

func TestRoundRobin_RotatesEveryRequest(t *testing.T) {
	store := newMemStore()
	eng := NewRoundRobinEngine(store)
	comboID := uuid.New()
	members := []combo.Member{testMember("a"), testMember("b"), testMember("c")}

	got := []string{}
	for i := 0; i < 6; i++ {
		m, err := eng.Select(context.Background(), comboID, members, 1)
		if err != nil {
			t.Fatalf("Select %d: %v", i, err)
		}
		got = append(got, m.ModelRef)
	}
	want := "a,b,c,a,b,c"
	if join(got) != want {
		t.Errorf("rotation = %s, want %s", join(got), want)
	}
}

func TestRoundRobin_StickyCount(t *testing.T) {
	store := newMemStore()
	eng := NewRoundRobinEngine(store)
	comboID := uuid.New()
	members := []combo.Member{testMember("a"), testMember("b")}

	got := []string{}
	for i := 0; i < 6; i++ {
		m, err := eng.Select(context.Background(), comboID, members, 2)
		if err != nil {
			t.Fatalf("Select %d: %v", i, err)
		}
		got = append(got, m.ModelRef)
	}
	want := "a,a,b,b,a,a"
	if join(got) != want {
		t.Errorf("sticky rotation = %s, want %s", join(got), want)
	}
}

func TestRoundRobin_RestartContinuesRotation(t *testing.T) {
	store := newMemStore()
	comboID := uuid.New()
	members := []combo.Member{testMember("a"), testMember("b")}

	first := NewRoundRobinEngine(store)
	if _, err := first.Select(context.Background(), comboID, members, 1); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, err := first.Select(context.Background(), comboID, members, 1); err != nil {
		t.Fatalf("select: %v", err)
	}

	restarted := NewRoundRobinEngine(store)
	m, err := restarted.Select(context.Background(), comboID, members, 1)
	if err != nil {
		t.Fatalf("restart select: %v", err)
	}
	// After a,b the cursor is back at index 0 => a.
	if m.ModelRef != "a" {
		t.Errorf("restart continuation = %s, want a", m.ModelRef)
	}
}

func TestRoundRobin_SkipsInactiveMembers(t *testing.T) {
	store := newMemStore()
	eng := NewRoundRobinEngine(store)
	comboID := uuid.New()
	active := testMember("a")
	inactive := testMember("b")
	inactive.IsActive = false
	members := []combo.Member{inactive, active}

	for i := 0; i < 3; i++ {
		m, err := eng.Select(context.Background(), comboID, members, 1)
		if err != nil {
			t.Fatalf("Select %d: %v", i, err)
		}
		if m.ModelRef != "a" {
			t.Errorf("selected inactive member %s", m.ModelRef)
		}
	}
}

func TestRoundRobin_NoEligibleMember(t *testing.T) {
	eng := NewRoundRobinEngine(newMemStore())
	_, err := eng.Select(context.Background(), uuid.New(),
		[]combo.Member{{ID: uuid.New(), IsActive: false}}, 1)
	if !errors.Is(err, ErrNoEligibleMember) {
		t.Fatalf("err = %v, want ErrNoEligibleMember", err)
	}
}

func TestRoundRobin_StoreErrorPropagates(t *testing.T) {
	store := &failingStore{}
	eng := NewRoundRobinEngine(store)
	_, err := eng.Select(context.Background(), uuid.New(), []combo.Member{testMember("a")}, 1)
	if err == nil {
		t.Fatal("expected store error")
	}
}

func TestRoundRobin_ConcurrentSelectsDeterministic(t *testing.T) {
	store := newMemStore()
	eng := NewRoundRobinEngine(store)
	comboID := uuid.New()
	members := []combo.Member{testMember("a"), testMember("b"), testMember("c")}

	const n = 12
	got := make([]string, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m, err := eng.Select(context.Background(), comboID, members, 1)
			if err != nil {
				errs <- err
				return
			}
			got[i] = m.ModelRef
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("select: %v", err)
	}

	counts := map[string]int{}
	for _, ref := range got {
		counts[ref]++
	}
	if counts["a"] != 4 || counts["b"] != 4 || counts["c"] != 4 {
		t.Errorf("rotation counts = %v, want a:4 b:4 c:4", counts)
	}
}

type failingStore struct{}

func (s *failingStore) GetRotation(ctx context.Context, comboID uuid.UUID) (*Rotation, error) {
	return nil, errors.New("store down")
}

func (s *failingStore) SaveRotation(ctx context.Context, comboID uuid.UUID, r Rotation) error {
	return errors.New("store down")
}

func join(refs []string) string {
	out := ""
	for i, r := range refs {
		if i > 0 {
			out += ","
		}
		out += r
	}
	return out
}
