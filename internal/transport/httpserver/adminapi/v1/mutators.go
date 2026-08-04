package v1

import (
	"context"
	"net/http"
)

// MutatorsService is the application seam for the routing mutator group. The
// frozen contract exposes mutator configuration through the settings
// surfaces; the mutator order itself is engine-owned (#162). This seam keeps
// the group's future wiring in one place.
type MutatorsService interface {
	List(ctx context.Context) (any, error)
	Set(ctx context.Context, name string, enabled bool) error
}

type mutatorsGroup struct{ svc MutatorsService }

func (g *mutatorsGroup) withSvc(w http.ResponseWriter, r *http.Request) bool {
	if g.svc == nil {
		backendUnavailable(w, r)
		return false
	}
	return true
}
