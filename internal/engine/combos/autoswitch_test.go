package combos

import (
	"context"
	"errors"
	"testing"

	"gorouter/internal/domain/combo"

	"github.com/google/uuid"
)

type fakeCatalog map[string][]string

func (c fakeCatalog) Supports(ctx context.Context, member combo.Member, capabilities []string) (bool, error) {
	have := c[member.ModelRef]
	for _, cap := range capabilities {
		found := false
		for _, h := range have {
			if h == cap {
				found = true
				break
			}
		}
		if !found {
			return false, nil
		}
	}
	return true, nil
}

type errCatalog struct{}

func (errCatalog) Supports(ctx context.Context, member combo.Member, capabilities []string) (bool, error) {
	return false, errors.New("catalog down")
}

func TestAutoSwitch_NoCapabilitiesKeepsOrder(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	eng := NewAutoSwitchEngine()
	m, err := eng.Select(context.Background(), members, nil, fakeCatalog{})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if m.ModelRef != "a" {
		t.Errorf("selected %s, want a", m.ModelRef)
	}
}

func TestAutoSwitch_SelectsCompatibleMember(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b"), testMember("c")}
	catalog := fakeCatalog{
		"a": {"chat"},
		"b": {"chat", "images"},
		"c": {"chat", "images", "audio"},
	}
	eng := NewAutoSwitchEngine()

	m, err := eng.Select(context.Background(), members, []string{"images"}, catalog)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if m.ModelRef != "b" {
		t.Errorf("selected %s, want b (first supporting images)", m.ModelRef)
	}

	m, err = eng.Select(context.Background(), members, []string{"audio"}, catalog)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if m.ModelRef != "c" {
		t.Errorf("selected %s, want c", m.ModelRef)
	}
}

func TestAutoSwitch_RequiresAllCapabilities(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	catalog := fakeCatalog{
		"a": {"chat", "images"},
		"b": {"chat"},
	}
	eng := NewAutoSwitchEngine()
	m, err := eng.Select(context.Background(), members, []string{"chat", "images"}, catalog)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if m.ModelRef != "a" {
		t.Errorf("selected %s, want a (only member with all capabilities)", m.ModelRef)
	}
}

func TestAutoSwitch_NoCompatibleRetainsFallbackOrder(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	catalog := fakeCatalog{"a": {"chat"}, "b": {"chat"}}
	eng := NewAutoSwitchEngine()
	m, err := eng.Select(context.Background(), members, []string{"video"}, catalog)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if m.ModelRef != "a" {
		t.Errorf("selected %s, want first fallback member a", m.ModelRef)
	}
}

func TestAutoSwitch_CatalogErrorFallsBack(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	eng := NewAutoSwitchEngine()
	m, err := eng.Select(context.Background(), members, []string{"images"}, errCatalog{})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if m.ModelRef != "a" {
		t.Errorf("selected %s, want fallback a", m.ModelRef)
	}
}

func TestAutoSwitch_NoEligibleMember(t *testing.T) {
	eng := NewAutoSwitchEngine()
	_, err := eng.Select(context.Background(),
		[]combo.Member{{ID: uuid.New(), IsActive: false}}, nil, fakeCatalog{})
	if !errors.Is(err, ErrNoEligibleMember) {
		t.Fatalf("err = %v, want ErrNoEligibleMember", err)
	}
}

func TestAutoSwitch_SkipsInactive(t *testing.T) {
	first := testMember("a")
	first.IsActive = false
	second := testMember("b")
	eng := NewAutoSwitchEngine()
	m, err := eng.Select(context.Background(), []combo.Member{first, second}, nil, fakeCatalog{})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if m.ModelRef != "b" {
		t.Errorf("selected %s, want b", m.ModelRef)
	}
}
