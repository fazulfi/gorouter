package v1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	appauth "gorouter/internal/app/auth"
	"gorouter/internal/app/tx"
	"gorouter/internal/shared/redact"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// AuditService is the application seam for the read-only audit group. The
// audit log is append-only; the contract exposes list/get/export only.
type AuditService interface {
	List(ctx context.Context, actor *appauth.Actor, filters tx.AuditFilters, page tx.AuditPage) ([]tx.AuditEntry, error)
	Export(ctx context.Context, actor *appauth.Actor, filters tx.AuditFilters) ([]tx.AuditEntry, error)
	Get(ctx context.Context, actor *appauth.Actor, id uuid.UUID) (*tx.AuditEntry, error)
}

type auditGroup struct{ svc AuditService }

// auditEntryView is the wire projection of an audit entry. Details pass
// through a redaction pass so credential-shaped remnants never resurface.
type auditEntryView struct {
	ID           uuid.UUID  `json:"id"`
	ActorID      *uuid.UUID `json:"actor_id,omitempty"`
	ActorKind    string     `json:"actor_kind"`
	JobID        *uuid.UUID `json:"job_id,omitempty"`
	Action       string     `json:"action"`
	ResourceType string     `json:"resource_type"`
	ResourceID   *uuid.UUID `json:"resource_id,omitempty"`
	IPAddress    string     `json:"ip_address,omitempty"`
	Details      string     `json:"details,omitempty"`
	OccurredAt   time.Time  `json:"occurred_at"`
}

func projectAuditEntry(e tx.AuditEntry) auditEntryView {
	ip := ""
	if e.IPAddress != nil {
		ip = e.IPAddress.String()
	}
	details := ""
	if len(e.Details) > 0 {
		details = redact.Redact(string(e.Details))
	}
	return auditEntryView{
		ID: e.ID, ActorID: e.ActorID, ActorKind: e.ActorKind, JobID: e.JobID,
		Action: e.Action, ResourceType: e.ResourceType, ResourceID: e.ResourceID,
		IPAddress: ip, Details: details, OccurredAt: e.OccurredAt,
	}
}

func (g *auditGroup) List(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	page := tx.AuditPage{
		Number: intQuery(r, "page", 0),
		Size:   intQuery(r, "size", 0),
	}
	rows, err := g.svc.List(r.Context(), actor, tx.AuditFilters{}, page)
	if err != nil {
		writeError(w, r, err)
		return
	}
	var out []auditEntryView
	for i := range rows {
		out = append(out, projectAuditEntry(rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

func (g *auditGroup) Get(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, r, errInvalid)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	entry, err := g.svc.Get(r.Context(), actor, id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, projectAuditEntry(*entry))
}

func (g *auditGroup) Export(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	rows, err := g.svc.Export(r.Context(), actor, tx.AuditFilters{})
	if err != nil {
		writeError(w, r, err)
		return
	}
	var out []auditEntryView
	for i := range rows {
		out = append(out, projectAuditEntry(rows[i]))
	}
	writeJSON(w, http.StatusOK, out)
}

// intQuery parses a non-negative integer query parameter with a default.
func intQuery(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return def
	}
	return n
}
