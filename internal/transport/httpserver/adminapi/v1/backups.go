package v1

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/backup"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// BackupDownload is the transport-side download result: the validated file
// path and metadata the handler streams to the caller. The backend service
// adapter maps its own result onto this shape at wiring time.
type BackupDownload struct {
	Path   string
	SHA256 string
	Bytes  int64
}

// BackupsService is the application seam for the backups group. The contract
// exposes list/download/verify only; restore is local CLI only (#202) and no
// restore route exists.
type BackupsService interface {
	List(ctx context.Context, actor *auth.Actor) ([]backup.Backup, error)
	Download(ctx context.Context, actor *auth.Actor, ip net.IP, id uuid.UUID) (*BackupDownload, error)
	Verify(ctx context.Context, actor *auth.Actor, id uuid.UUID) error
}

type backupsGroup struct{ svc BackupsService }

func (g *backupsGroup) List(w http.ResponseWriter, r *http.Request) {
	actor := actorFrom(r)
	if actor == nil {
		writeError(w, r, errUnauthorized)
		return
	}
	if g.svc == nil {
		backendUnavailable(w, r)
		return
	}
	rows, err := g.svc.List(r.Context(), actor)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (g *backupsGroup) Download(w http.ResponseWriter, r *http.Request) {
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
	res, err := g.svc.Download(r.Context(), actor, net.ParseIP(clientIP(r)), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	if res == nil || res.Path == "" {
		backendUnavailable(w, r)
		return
	}
	f, err := os.Open(res.Path)
	if err != nil {
		writeError(w, r, err)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(res.Bytes, 10))
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, f)
}

func (g *backupsGroup) Verify(w http.ResponseWriter, r *http.Request) {
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
	if err := g.svc.Verify(r.Context(), actor, id); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "verified"})
}
