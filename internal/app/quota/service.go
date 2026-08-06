package quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"gorouter/internal/app/cooldown"
	"gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/quota"

	"github.com/google/uuid"
)

var (
	errActorRequired   = errors.New("quota: actor is required")
	errProviderIDNil   = errors.New("quota: provider ID is required")
	errInvalidKind     = errors.New("quota: error kind must be temporary or definitive")
	errWindowStartZero = errors.New("quota: window start is required")
)

// windowState is the quota-specific per-provider state the service owns. The
// failure/cooldown fields deliberately live in the reused cooldown registry
// (keyed by provider ID) rather than here, so no parallel cooldown store
// exists.
type windowState struct {
	windowStart  time.Time
	pingLead     time.Duration
	refreshAhead time.Duration
	errorKind    quota.ErrorKind
}

// QuotaService exposes the per-provider quota tracker state (P4-T08). The
// CH-08 auto-ping job records window and ping outcomes through
// RecordWindow/RecordPingSuccess/RecordPingFailure and reads the surface back
// through Status/Countdown; the admin surface unlocks and resets a single
// provider through Unlock/Reset, both audited in the same transaction. There
// is no global reset or unlock path (DECISIONS #176): every mutation is
// scoped to exactly one provider ID, and the nil UUID is rejected.
type QuotaService struct {
	mu       sync.RWMutex
	windows  map[uuid.UUID]*windowState
	cooldown *cooldown.Registry
	beginner QuotaScopeBeginner
	now      func() time.Time
}

// NewQuotaService creates a quota service over the given scope beginner. The
// failure cooldown reuses the cooldown registry machinery with the frozen
// quota configuration (audit/11 section 7d): a single failure arms a flat
// 15-minute cooldown.
func NewQuotaService(beginner QuotaScopeBeginner) *QuotaService {
	return &QuotaService{
		windows: make(map[uuid.UUID]*windowState),
		cooldown: cooldown.New(cooldown.Config{
			DefaultCooldown:  quota.DefaultFailureCooldown,
			MaxCooldown:      quota.DefaultFailureCooldown,
			FailureThreshold: 1,
			EscalationFactor: 1.0,
			CleanupInterval:  5 * time.Minute,
		}),
		beginner: beginner,
		now:      time.Now,
	}
}

// WithClock replaces the internal clock (tests only; the default is
// time.Now). The reused cooldown registry is driven by the same clock so all
// cooldown timing stays deterministic. All timestamps are UTC.
func (s *QuotaService) WithClock(now func() time.Time) {
	s.now = now
	s.cooldown.WithClock(now)
}

// Status returns the quota status surface for exactly one provider, or nil
// when the provider has never been observed (no window and no cooldown
// state). Status is an authenticated read: a nil actor is rejected. The
// returned status is a fresh copy; callers cannot mutate service state.
func (s *QuotaService) Status(ctx context.Context, actor *auth.Actor, providerID uuid.UUID) (*quota.QuotaStatus, error) {
	if actor == nil {
		return nil, errActorRequired
	}
	if providerID == uuid.Nil {
		return nil, errProviderIDNil
	}
	s.mu.RLock()
	w, okW := s.windows[providerID]
	var snap *windowState
	if okW {
		cp := *w
		snap = &cp
	}
	s.mu.RUnlock()
	cd := s.cooldown.Status(ctx, providerID)
	if !okW && cd == nil {
		return nil, nil
	}
	st := &quota.QuotaStatus{ProviderID: providerID}
	if snap != nil {
		st.WindowStart = snap.windowStart
		st.PingLead = snap.pingLead
		st.RefreshAhead = snap.refreshAhead
		st.ErrorKind = snap.errorKind
	}
	if cd != nil {
		st.CooldownUntil = cd.ExpiresAt
		st.LastError = sanitizeLastError(cd.Reason)
	}
	return st, nil
}

// Countdown returns the time until the provider's current 5-hour window
// resets (WindowStart + WindowDuration minus now), floored at zero — a
// negative countdown is never returned. Providers with no observed window
// count down to zero. Countdown is a mechanical read (no actor): the UI and
// the auto-ping job both call it without identity.
func (s *QuotaService) Countdown(ctx context.Context, providerID uuid.UUID) time.Duration {
	if providerID == uuid.Nil {
		return 0
	}
	s.mu.RLock()
	w, ok := s.windows[providerID]
	var start time.Time
	if ok {
		start = w.windowStart
	}
	s.mu.RUnlock()
	if !ok || start.IsZero() {
		return 0
	}
	d := start.Add(quota.WindowDuration).Sub(s.now())
	if d < 0 {
		return 0
	}
	return d
}

// Unlock clears the failure cooldown for exactly one provider: cooldown,
// last error and error kind are cleared while the current 5-hour window is
// preserved. Unlock requires an actor, is scoped to exactly one provider ID,
// and is audited in the same transaction as the unlock itself: the audit row
// (actor ID, scoped provider target, sanitized before/after) is committed
// before the in-memory cooldown state is touched, so an audit or commit
// failure rolls the unlock back. There is no unlock-all path.
func (s *QuotaService) Unlock(ctx context.Context, actor *auth.Actor, providerID uuid.UUID) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	if providerID == uuid.Nil {
		return errProviderIDNil
	}
	before := s.auditState(ctx, providerID)
	after := quota.QuotaStatus{ProviderID: providerID, WindowStart: before.WindowStart,
		PingLead: before.PingLead, RefreshAhead: before.RefreshAhead}
	if err := s.auditedMutation(ctx, actor, "quota.unlock", providerID, before, after); err != nil {
		return err
	}
	s.cooldown.RecordSuccess(ctx, providerID)
	s.mu.Lock()
	if w, ok := s.windows[providerID]; ok {
		w.errorKind = ""
	}
	s.mu.Unlock()
	return nil
}

// Reset clears the whole quota state for exactly one provider: the failure
// cooldown is cleared and the current 5-hour window restarts from now (the
// upstream "reset" semantics of starting a fresh window immediately). Reset
// requires an actor, is scoped to exactly one provider ID, and is audited in
// the same transaction with the same rollback guarantees as Unlock. There is
// no global reset path (DECISIONS #176).
func (s *QuotaService) Reset(ctx context.Context, actor *auth.Actor, providerID uuid.UUID) error {
	if err := requireActor(actor); err != nil {
		return err
	}
	if providerID == uuid.Nil {
		return errProviderIDNil
	}
	before := s.auditState(ctx, providerID)
	now := s.now().UTC()
	after := quota.QuotaStatus{ProviderID: providerID, WindowStart: now,
		PingLead: before.PingLead, RefreshAhead: before.RefreshAhead}
	if err := s.auditedMutation(ctx, actor, "quota.reset", providerID, before, after); err != nil {
		return err
	}
	s.cooldown.RecordSuccess(ctx, providerID)
	s.mu.Lock()
	s.windows[providerID] = &windowState{windowStart: now, pingLead: before.PingLead, refreshAhead: before.RefreshAhead}
	s.mu.Unlock()
	return nil
}

// RecordWindow stores the provider's current 5-hour quota window start and
// the ping configuration observed by the auto-ping job. Zero ping lead or
// refresh-ahead values fall back to the frozen defaults. The job's next
// successful usage fetch overwrites the window. Recording is job telemetry
// and is deliberately not audited here (the CH-08 job audits its own runs via
// job provenance).
func (s *QuotaService) RecordWindow(ctx context.Context, providerID uuid.UUID, windowStart time.Time, pingLead, refreshAhead time.Duration) error {
	if providerID == uuid.Nil {
		return errProviderIDNil
	}
	if windowStart.IsZero() {
		return errWindowStartZero
	}
	if pingLead <= 0 {
		pingLead = quota.DefaultPingLead
	}
	if refreshAhead <= 0 {
		refreshAhead = quota.DefaultRefreshAhead
	}
	s.mu.Lock()
	if w, ok := s.windows[providerID]; ok {
		w.windowStart = windowStart.UTC()
		w.pingLead = pingLead
		w.refreshAhead = refreshAhead
	} else {
		s.windows[providerID] = &windowState{
			windowStart:  windowStart.UTC(),
			pingLead:     pingLead,
			refreshAhead: refreshAhead,
		}
	}
	s.mu.Unlock()
	return nil
}

// RecordPingSuccess records a successful auto-ping: the failure cooldown,
// last error and error kind are cleared for the provider. Job telemetry, not
// audited here.
func (s *QuotaService) RecordPingSuccess(ctx context.Context, providerID uuid.UUID) error {
	if providerID == uuid.Nil {
		return errProviderIDNil
	}
	s.cooldown.RecordSuccess(ctx, providerID)
	s.mu.Lock()
	if w, ok := s.windows[providerID]; ok {
		w.errorKind = ""
	}
	s.mu.Unlock()
	return nil
}

// RecordPingFailure records a failed auto-ping: the sanitized error message
// is stored, the error kind is frozen (temporary or definitive), and the
// 15-minute failure cooldown is armed through the reused cooldown registry.
// Both kinds arm the same cooldown; the kind only drives presentation
// (DECISIONS #168, #353). Job telemetry, not audited here.
func (s *QuotaService) RecordPingFailure(ctx context.Context, providerID uuid.UUID, err error, kind quota.ErrorKind) error {
	if providerID == uuid.Nil {
		return errProviderIDNil
	}
	if !kind.IsValid() {
		return errInvalidKind
	}
	s.cooldown.RecordFailure(ctx, providerID, errors.New(sanitizeLastError(errMessage(err))))
	s.mu.Lock()
	if w, ok := s.windows[providerID]; ok {
		w.errorKind = kind
	} else {
		s.windows[providerID] = &windowState{errorKind: kind}
	}
	s.mu.Unlock()
	return nil
}

// auditedMutation writes the sanitized audit entry and commits it before the
// caller applies the in-memory mutation, so any audit or commit failure
// leaves the service state untouched (the mutation is rolled back).
func (s *QuotaService) auditedMutation(ctx context.Context, actor *auth.Actor, action string, providerID uuid.UUID, before, after quota.QuotaStatus) error {
	scope, err := s.beginner.Begin(ctx)
	if err != nil {
		return fmt.Errorf("quota: begin tx: %w", err)
	}
	defer func() { _ = scope.Rollback(ctx) }()
	entry := &tx.AuditLogEntry{
		ID:           uuid.New(),
		ActorID:      &actor.UserID,
		Action:       action,
		ResourceType: "provider",
		ResourceID:   &providerID,
		OccurredAt:   s.now().UTC(),
	}
	details := map[string]any{
		"before": statusAuditView(before),
		"after":  statusAuditView(after),
	}
	if b, err := json.Marshal(details); err == nil {
		entry.Details = b
	}
	if err := scope.AuditLog().Create(ctx, entry); err != nil {
		return fmt.Errorf("quota: audit %s: %w", action, err)
	}
	if err := scope.Commit(ctx); err != nil {
		return fmt.Errorf("quota: commit tx: %w", err)
	}
	return nil
}

// auditState snapshots the provider's status for a before/after audit view.
func (s *QuotaService) auditState(ctx context.Context, providerID uuid.UUID) quota.QuotaStatus {
	st := quota.QuotaStatus{ProviderID: providerID}
	s.mu.RLock()
	if w, ok := s.windows[providerID]; ok {
		st.WindowStart = w.windowStart
		st.PingLead = w.pingLead
		st.RefreshAhead = w.refreshAhead
		st.ErrorKind = w.errorKind
	}
	s.mu.RUnlock()
	if cd := s.cooldown.Status(ctx, providerID); cd != nil {
		st.CooldownUntil = cd.ExpiresAt
	}
	return st
}

// statusAuditView is the sanitized audit projection of a status: timestamps
// and kinds only. Raw error text and any credential-bearing strings never
// reach the audit trail.
func statusAuditView(st quota.QuotaStatus) map[string]any {
	return map[string]any{
		"window_start":   st.WindowStart.UTC().Format(time.RFC3339Nano),
		"cooldown_until": st.CooldownUntil.UTC().Format(time.RFC3339Nano),
		"error_kind":     string(st.ErrorKind),
	}
}

func requireActor(actor *auth.Actor) error {
	if actor == nil {
		return errActorRequired
	}
	return nil
}

func errMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

var (
	credentialPattern = regexp.MustCompile(`(?i)(bearer\s+[a-z0-9._~+/=-]+|sk-[a-z0-9]{6,}|(?:api[_-]?key|access[_-]?token|refresh[_-]?token|token|password|secret|credential|authorization)\s*[:=]\s*[^\s,;]+)`)
)

// sanitizeLastError strips credential-shaped segments (Bearer tokens,
// sk-... keys, key=value credentials) and truncates the message so raw
// provider error strings, tokens or credentials never surface in status or
// audit (decision #127 no-secrets contract).
func sanitizeLastError(msg string) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return ""
	}
	msg = credentialPattern.ReplaceAllString(msg, "[REDACTED]")
	msg = strings.Join(strings.Fields(msg), " ")
	if len(msg) > 500 {
		msg = msg[:500]
	}
	return msg
}
