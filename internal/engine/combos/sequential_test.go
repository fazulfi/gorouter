package combos

import (
	"context"
	"errors"
	"testing"

	"gorouter/internal/domain/combo"

	"github.com/google/uuid"
)

func testMember(model string) combo.Member {
	return combo.Member{
		ID:         uuid.New(),
		ComboID:    uuid.New(),
		ProviderID: uuid.New(),
		ModelRef:   model,
		Priority:   0,
		Weight:     1,
		IsActive:   true,
	}
}

var testRequest = []byte(`{"model":"test-request"}`)

func TestSequential_SuccessOnFirst(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	invoked := 0
	res, err := NewSequentialEngine().Run(context.Background(), members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			invoked++
			return []byte("ok-a"), nil
		}, DefaultFailureClassifier)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Member.ModelRef != "a" || string(res.Body) != "ok-a" {
		t.Errorf("result = %+v", res)
	}
	if invoked != 1 {
		t.Errorf("invoked = %d, want 1", invoked)
	}
}

func TestSequential_ForwardsRequestBody(t *testing.T) {
	members := []combo.Member{testMember("a")}
	got := []byte{}
	_, err := NewSequentialEngine().Run(context.Background(), members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			got = append(got, req...)
			return []byte("ok"), nil
		}, DefaultFailureClassifier)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(got) != string(testRequest) {
		t.Errorf("request not forwarded: %s", got)
	}
}

func TestSequential_FallsBackOnEligibleFailure(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b"), testMember("c")}
	res, err := NewSequentialEngine().Run(context.Background(), members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			if m.ModelRef == "c" {
				return []byte("ok-c"), nil
			}
			return nil, errors.New("transient failure")
		}, DefaultFailureClassifier)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Member.ModelRef != "c" {
		t.Errorf("expected fallback to c, got %s", res.Member.ModelRef)
	}
}

func TestSequential_DefinitiveFailureStops(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	invoked := []string{}
	_, err := NewSequentialEngine().Run(context.Background(), members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			invoked = append(invoked, m.ModelRef)
			return nil, ErrDefinitiveFailure
		}, DefaultFailureClassifier)
	if !errors.Is(err, ErrDefinitiveFailure) {
		t.Fatalf("err = %v, want ErrDefinitiveFailure", err)
	}
	if len(invoked) != 1 {
		t.Errorf("invoked = %v, want only first member", invoked)
	}
}

func TestSequential_AllFailReturnsLastError(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	_, err := NewSequentialEngine().Run(context.Background(), members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			return nil, errors.New("boom-" + m.ModelRef)
		}, DefaultFailureClassifier)
	if err == nil || err.Error() != "boom-b" {
		t.Errorf("err = %v, want last member error", err)
	}
}

func TestSequential_CancellationPropagates(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewSequentialEngine().Run(ctx, members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			return []byte("unreachable"), nil
		}, DefaultFailureClassifier)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestSequential_NoEligibleMember(t *testing.T) {
	members := []combo.Member{{ID: uuid.New(), IsActive: false}}
	_, err := NewSequentialEngine().Run(context.Background(), members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) { return nil, nil }, nil)
	if !errors.Is(err, ErrNoEligibleMember) {
		t.Fatalf("err = %v, want ErrNoEligibleMember", err)
	}
}

func TestSequential_NilClassifierNeverFallsBack(t *testing.T) {
	members := []combo.Member{testMember("a"), testMember("b")}
	_, err := NewSequentialEngine().Run(context.Background(), members, testRequest,
		func(ctx context.Context, m combo.Member, req []byte) ([]byte, error) {
			return nil, errors.New("any error")
		}, nil)
	if err == nil || err.Error() != "any error" {
		t.Errorf("err = %v, want first member error without fallback", err)
	}
}

func TestDefaultFailureClassifier(t *testing.T) {
	if DefaultFailureClassifier(errors.New("transient")) != true {
		t.Error("transient must be fallback-eligible")
	}
	if DefaultFailureClassifier(ErrDefinitiveFailure) != false {
		t.Error("definitive must not be fallback-eligible")
	}
}
