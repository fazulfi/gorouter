package quota

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/quota"

	"github.com/google/uuid"
)

var (
	testActor = &auth.Actor{UserID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), SessionID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), IsAdmin: true}
	providerA = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	providerB = uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	testBase  = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
)

type fakeAuditRepo struct {
	entries []*tx.AuditLogEntry
	err     error
}

func (f *fakeAuditRepo) Create(_ context.Context, entry *tx.AuditLogEntry) error {
	if f.err != nil {
		return f.err
	}
	f.entries = append(f.entries, entry)
	return nil
}

type fakeScope struct {
	audit         *fakeAuditRepo
	commitErr     error
	commitCalls   int
	rollbackCalls int
	beginErr      error
}

func (f *fakeScope) AuditLog() tx.AuditLogRepository { return f.audit }

func (f *fakeScope) Commit(context.Context) error {
	f.commitCalls++
	return f.commitErr
}

func (f *fakeScope) Rollback(context.Context) error {
	f.rollbackCalls++
	return nil
}

// newTestService builds a service with a fixed clock and a recording scope.
func newTestService() (*QuotaService, *fakeScope) {
	scope := &fakeScope{audit: &fakeAuditRepo{}}
	svc := NewQuotaService(QuotaScopeBeginnerFunc(func(context.Context) (QuotaScope, error) {
		if scope.beginErr != nil {
			return nil, scope.beginErr
		}
		return scope, nil
	}))
	svc.WithClock(func() time.Time { return testBase })
	return svc, scope
}

// seedFailure puts a provider into failure cooldown with a window recorded.
func seedFailure(t *testing.T, svc *QuotaService, providerID uuid.UUID, kind quota.ErrorKind) {
	t.Helper()
	if err := svc.RecordWindow(context.Background(), providerID, testBase.Add(-1*time.Hour), 0, 0); err != nil {
		t.Fatalf("seed window: %v", err)
	}
	if err := svc.RecordPingFailure(context.Background(), providerID, errors.New("upstream 500"), kind); err != nil {
		t.Fatalf("seed failure: %v", err)
	}
}

func TestQuotaUnlockAudited(t *testing.T) {
	t.Run("nil actor rejected", func(t *testing.T) {
		svc, scope := newTestService()
		seedFailure(t, svc, providerA, quota.ErrorKindTemporary)
		err := svc.Unlock(context.Background(), nil, providerA)
		if err == nil {
			t.Fatal("nil actor must be rejected")
		}
		if len(scope.audit.entries) != 0 {
			t.Fatalf("nil actor must not audit, got %d entries", len(scope.audit.entries))
		}
		st, err := svc.Status(context.Background(), testActor, providerA)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st == nil || st.CooldownUntil.IsZero() {
			t.Fatal("nil actor unlock must leave cooldown armed")
		}
	})

	t.Run("unlocks exactly one provider", func(t *testing.T) {
		svc, _ := newTestService()
		seedFailure(t, svc, providerA, quota.ErrorKindTemporary)
		seedFailure(t, svc, providerB, quota.ErrorKindDefinitive)
		if err := svc.Unlock(context.Background(), testActor, providerA); err != nil {
			t.Fatalf("unlock: %v", err)
		}
		stA, err := svc.Status(context.Background(), testActor, providerA)
		if err != nil {
			t.Fatalf("status A: %v", err)
		}
		if stA == nil {
			t.Fatal("unlocked provider must still expose a status (window preserved)")
		}
		if !stA.CooldownUntil.IsZero() || stA.ErrorKind != "" || stA.LastError != "" {
			t.Fatalf("provider A must be fully unlocked, got %+v", stA)
		}
		if stA.WindowStart.IsZero() {
			t.Fatal("unlock must preserve the current window")
		}
		stB, err := svc.Status(context.Background(), testActor, providerB)
		if err != nil {
			t.Fatalf("status B: %v", err)
		}
		if stB == nil || stB.CooldownUntil.IsZero() || stB.ErrorKind != quota.ErrorKindDefinitive {
			t.Fatalf("provider B must remain untouched, got %+v", stB)
		}
	})

	t.Run("no global reset or unlock path", func(t *testing.T) {
		svc, scope := newTestService()
		seedFailure(t, svc, providerA, quota.ErrorKindTemporary)
		for _, mut := range []func() error{
			func() error { return svc.Unlock(context.Background(), testActor, uuid.Nil) },
			func() error { return svc.Reset(context.Background(), testActor, uuid.Nil) },
		} {
			if err := mut(); err == nil {
				t.Fatal("nil provider ID must be rejected")
			}
		}
		if len(scope.audit.entries) != 0 {
			t.Fatal("rejected mutations must not audit")
		}
		st, err := svc.Status(context.Background(), testActor, providerA)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st == nil || st.CooldownUntil.IsZero() {
			t.Fatal("nil-ID attempts must not clear any provider state")
		}
	})

	t.Run("audits actor, scoped target and sanitized details in same transaction", func(t *testing.T) {
		svc, scope := newTestService()
		seedFailure(t, svc, providerA, quota.ErrorKindTemporary)
		if err := svc.Unlock(context.Background(), testActor, providerA); err != nil {
			t.Fatalf("unlock: %v", err)
		}
		if len(scope.audit.entries) != 1 {
			t.Fatalf("want exactly 1 audit entry, got %d", len(scope.audit.entries))
		}
		e := scope.audit.entries[0]
		if e.ActorID == nil || *e.ActorID != testActor.UserID {
			t.Fatalf("audit must carry the actor ID, got %v", e.ActorID)
		}
		if e.ResourceID == nil || *e.ResourceID != providerA {
			t.Fatalf("audit must target exactly provider A, got %v", e.ResourceID)
		}
		if e.ResourceType != "provider" || e.Action != "quota.unlock" {
			t.Fatalf("audit resource/action = %s/%s", e.ResourceType, e.Action)
		}
		var details map[string]any
		if err := json.Unmarshal(e.Details, &details); err != nil {
			t.Fatalf("audit details must be JSON: %v", err)
		}
		if _, ok := details["before"]; !ok {
			t.Fatal("audit details must carry before")
		}
		if _, ok := details["after"]; !ok {
			t.Fatal("audit details must carry after")
		}
		if scope.commitCalls != 1 {
			t.Fatalf("unlock must commit exactly once, got %d", scope.commitCalls)
		}
	})

	t.Run("audit failure rolls back unlock", func(t *testing.T) {
		svc, scope := newTestService()
		seedFailure(t, svc, providerA, quota.ErrorKindTemporary)
		scope.audit.err = errors.New("audit write failed")
		if err := svc.Unlock(context.Background(), testActor, providerA); err == nil {
			t.Fatal("audit failure must fail the unlock")
		}
		st, err := svc.Status(context.Background(), testActor, providerA)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st == nil || st.CooldownUntil.IsZero() {
			t.Fatal("audit failure must roll the unlock back")
		}
		if scope.rollbackCalls != 1 {
			t.Fatalf("audit failure must roll back the tx, got %d rollbacks", scope.rollbackCalls)
		}
	})

	t.Run("commit failure rolls back unlock", func(t *testing.T) {
		svc, scope := newTestService()
		seedFailure(t, svc, providerA, quota.ErrorKindTemporary)
		scope.commitErr = errors.New("commit failed")
		if err := svc.Unlock(context.Background(), testActor, providerA); err == nil {
			t.Fatal("commit failure must fail the unlock")
		}
		st, err := svc.Status(context.Background(), testActor, providerA)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st == nil || st.CooldownUntil.IsZero() {
			t.Fatal("commit failure must roll the unlock back")
		}
		if scope.commitCalls != 1 || scope.rollbackCalls != 1 {
			t.Fatalf("commit failure must commit-then-rollback, got %d/%d", scope.commitCalls, scope.rollbackCalls)
		}
	})
}

func TestQuotaStatus(t *testing.T) {
	t.Run("nil actor rejected", func(t *testing.T) {
		svc, _ := newTestService()
		if _, err := svc.Status(context.Background(), nil, providerA); err == nil {
			t.Fatal("nil actor must be rejected")
		}
	})

	t.Run("nil provider rejected", func(t *testing.T) {
		svc, _ := newTestService()
		if _, err := svc.Status(context.Background(), testActor, uuid.Nil); err == nil {
			t.Fatal("nil provider ID must be rejected")
		}
	})

	t.Run("unknown provider returns nil status", func(t *testing.T) {
		svc, _ := newTestService()
		st, err := svc.Status(context.Background(), testActor, providerA)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st != nil {
			t.Fatalf("never-observed provider must return nil status, got %+v", st)
		}
	})

	t.Run("full surface after window and failure", func(t *testing.T) {
		svc, _ := newTestService()
		seedFailure(t, svc, providerA, quota.ErrorKindDefinitive)
		st, err := svc.Status(context.Background(), testActor, providerA)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st == nil {
			t.Fatal("observed provider must return a status")
		}
		if st.ProviderID != providerA {
			t.Fatalf("provider ID = %s", st.ProviderID)
		}
		if want := testBase.Add(-1 * time.Hour); !st.WindowStart.Equal(want) {
			t.Fatalf("window start = %s, want %s", st.WindowStart, want)
		}
		if st.PingLead != quota.DefaultPingLead || st.RefreshAhead != quota.DefaultRefreshAhead {
			t.Fatalf("frozen defaults not applied: %s/%s", st.PingLead, st.RefreshAhead)
		}
		if want := testBase.Add(quota.DefaultFailureCooldown); !st.CooldownUntil.Equal(want) {
			t.Fatalf("cooldown until = %s, want %s", st.CooldownUntil, want)
		}
		if st.ErrorKind != quota.ErrorKindDefinitive {
			t.Fatalf("error kind = %s", st.ErrorKind)
		}
	})

	t.Run("returned status is a copy", func(t *testing.T) {
		svc, _ := newTestService()
		seedFailure(t, svc, providerA, quota.ErrorKindTemporary)
		st1, err := svc.Status(context.Background(), testActor, providerA)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		st1.WindowStart = time.Time{}
		st1.LastError = "mutated"
		st1.ErrorKind = ""
		st2, err := svc.Status(context.Background(), testActor, providerA)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st2.WindowStart.IsZero() || st2.LastError == "mutated" || st2.ErrorKind != quota.ErrorKindTemporary {
			t.Fatal("mutating a returned status must not affect service state")
		}
	})
}

func TestQuotaCountdown(t *testing.T) {
	svc, _ := newTestService()
	if d := svc.Countdown(context.Background(), uuid.Nil); d != 0 {
		t.Fatalf("nil provider countdown = %s, want 0", d)
	}
	if d := svc.Countdown(context.Background(), providerA); d != 0 {
		t.Fatalf("unknown provider countdown = %s, want 0", d)
	}
	if err := svc.RecordWindow(context.Background(), providerA, testBase.Add(-1*time.Hour), 0, 0); err != nil {
		t.Fatalf("record window: %v", err)
	}
	if d := svc.Countdown(context.Background(), providerA); d != 4*time.Hour {
		t.Fatalf("countdown = %s, want 4h", d)
	}
	if err := svc.RecordWindow(context.Background(), providerA, testBase.Add(-6*time.Hour), 0, 0); err != nil {
		t.Fatalf("record window: %v", err)
	}
	if d := svc.Countdown(context.Background(), providerA); d != 0 {
		t.Fatalf("elapsed window must floor at zero, got %s", d)
	}
}

func TestQuotaReset(t *testing.T) {
	t.Run("restarts window and clears cooldown", func(t *testing.T) {
		svc, scope := newTestService()
		seedFailure(t, svc, providerA, quota.ErrorKindTemporary)
		if err := svc.Reset(context.Background(), testActor, providerA); err != nil {
			t.Fatalf("reset: %v", err)
		}
		if len(scope.audit.entries) != 1 || scope.audit.entries[0].Action != "quota.reset" {
			t.Fatalf("reset must audit quota.reset, got %d entries", len(scope.audit.entries))
		}
		st, err := svc.Status(context.Background(), testActor, providerA)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st == nil {
			t.Fatal("reset must leave a fresh window status")
		}
		if !st.CooldownUntil.IsZero() || st.ErrorKind != "" || st.LastError != "" {
			t.Fatalf("reset must clear cooldown state, got %+v", st)
		}
		if !st.WindowStart.Equal(testBase) {
			t.Fatalf("reset must restart the window at now, got %s", st.WindowStart)
		}
		if d := svc.Countdown(context.Background(), providerA); d != quota.WindowDuration {
			t.Fatalf("fresh window countdown = %s, want %s", d, quota.WindowDuration)
		}
	})

	t.Run("audit failure rolls back reset", func(t *testing.T) {
		svc, scope := newTestService()
		seedFailure(t, svc, providerA, quota.ErrorKindTemporary)
		scope.audit.err = errors.New("audit write failed")
		if err := svc.Reset(context.Background(), testActor, providerA); err == nil {
			t.Fatal("audit failure must fail the reset")
		}
		st, err := svc.Status(context.Background(), testActor, providerA)
		if err != nil {
			t.Fatalf("status: %v", err)
		}
		if st == nil || st.CooldownUntil.IsZero() || st.WindowStart.Equal(testBase) {
			t.Fatal("audit failure must roll the reset back")
		}
	})

	t.Run("nil actor rejected", func(t *testing.T) {
		svc, scope := newTestService()
		if err := svc.Reset(context.Background(), nil, providerA); err == nil {
			t.Fatal("nil actor must be rejected")
		}
		if len(scope.audit.entries) != 0 {
			t.Fatal("nil actor must not audit")
		}
	})
}

func TestQuotaRecordWindow(t *testing.T) {
	svc, _ := newTestService()
	if err := svc.RecordWindow(context.Background(), uuid.Nil, testBase, 0, 0); err == nil {
		t.Fatal("nil provider must be rejected")
	}
	if err := svc.RecordWindow(context.Background(), providerA, time.Time{}, 0, 0); err == nil {
		t.Fatal("zero window start must be rejected")
	}
	start := testBase.Add(-30 * time.Minute)
	if err := svc.RecordWindow(context.Background(), providerA, start, 0, 0); err != nil {
		t.Fatalf("record window: %v", err)
	}
	st, err := svc.Status(context.Background(), testActor, providerA)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st == nil || !st.WindowStart.Equal(start) {
		t.Fatalf("window start not preserved: %+v", st)
	}
	if st.PingLead != quota.DefaultPingLead || st.RefreshAhead != quota.DefaultRefreshAhead {
		t.Fatalf("zero durations must fall back to frozen defaults, got %s/%s", st.PingLead, st.RefreshAhead)
	}
	lead, ahead := 7*time.Second, 9*time.Minute
	if err := svc.RecordWindow(context.Background(), providerA, start.Add(time.Minute), lead, ahead); err != nil {
		t.Fatalf("record window: %v", err)
	}
	st, _ = svc.Status(context.Background(), testActor, providerA)
	if st.PingLead != lead || st.RefreshAhead != ahead {
		t.Fatalf("explicit durations not preserved, got %s/%s", st.PingLead, st.RefreshAhead)
	}
}

func TestQuotaErrorKindMapping(t *testing.T) {
	svc, _ := newTestService()
	if err := svc.RecordPingFailure(context.Background(), providerA, errors.New("boom"), quota.ErrorKind("transient")); err == nil {
		t.Fatal("invalid kind must be rejected")
	}
	if err := svc.RecordPingFailure(context.Background(), providerA, errors.New("boom"), quota.ErrorKindDefinitive); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	st, err := svc.Status(context.Background(), testActor, providerA)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st == nil || st.ErrorKind != quota.ErrorKindDefinitive || st.CooldownUntil.IsZero() {
		t.Fatalf("definitive failure must surface kind + cooldown, got %+v", st)
	}
	if err := svc.RecordPingSuccess(context.Background(), providerA); err != nil {
		t.Fatalf("record success: %v", err)
	}
	st, _ = svc.Status(context.Background(), testActor, providerA)
	if st == nil || st.ErrorKind != "" || !st.CooldownUntil.IsZero() || st.LastError != "" {
		t.Fatalf("success must clear kind + cooldown + error, got %+v", st)
	}
}

func TestQuotaNoSecrets(t *testing.T) {
	svc, scope := newTestService()
	skTail := "ABCDEF" + "12345678901234"
	leaky := errors.New("upstream auth failed: Bearer abcdef1234567890 token=xyz-secret sk-" + skTail)
	if err := svc.RecordPingFailure(context.Background(), providerA, leaky, quota.ErrorKindDefinitive); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	st, err := svc.Status(context.Background(), testActor, providerA)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, secret := range []string{"abcdef1234567890", "xyz-secret", skTail} {
		if strings.Contains(st.LastError, secret) {
			t.Fatalf("status must not leak secret %q, got %q", secret, st.LastError)
		}
	}
	if st.LastError == "" {
		t.Fatal("sanitized error must still carry the sanitized message")
	}
	if err := svc.Unlock(context.Background(), testActor, providerA); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	for _, entry := range scope.audit.entries {
		if strings.Contains(string(entry.Details), "abcdef1234567890") ||
			strings.Contains(string(entry.Details), "xyz-secret") {
			t.Fatalf("audit details must be sanitized, got %s", entry.Details)
		}
	}
}

func TestQuotaConcurrentAccess(t *testing.T) {
	svc, _ := newTestService()
	start := testBase.Add(-2 * time.Hour)
	if err := svc.RecordWindow(context.Background(), providerA, start, 0, 0); err != nil {
		t.Fatalf("record window: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				switch (i + j) % 4 {
				case 0:
					_, _ = svc.Status(context.Background(), testActor, providerA)
				case 1:
					_ = svc.Countdown(context.Background(), providerA)
				case 2:
					_ = svc.RecordPingSuccess(context.Background(), providerA)
				default:
					_ = svc.RecordPingFailure(context.Background(), providerA, errors.New("boom"), quota.ErrorKindTemporary)
				}
			}
		}(i)
	}
	wg.Wait()
	st, err := svc.Status(context.Background(), testActor, providerA)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st == nil {
		t.Fatal("provider must still have state after concurrent access")
	}
	if !st.WindowStart.Equal(start) {
		t.Fatalf("window must survive concurrent access, got %s", st.WindowStart)
	}
}
