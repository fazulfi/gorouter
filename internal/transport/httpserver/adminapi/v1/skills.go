package v1

import (
	"context"
)

// Skill is one entry of the static read-only skills catalog (design §7:
// skills is a static catalog of install links and instructions, never a
// mutation surface).
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	InstallURL  string `json:"install_url,omitempty"`
}

// SkillsService is the application seam for the static skills catalog. The
// frozen contract exposes no /skills route (read-only catalog); this seam
// keeps the group's future wiring in one place.
type SkillsService interface {
	Catalog(ctx context.Context) ([]Skill, error)
}

type skillsGroup struct{ svc SkillsService }
