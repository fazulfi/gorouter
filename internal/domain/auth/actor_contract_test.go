package auth

import (
	"testing"

	"github.com/google/uuid"
)

// TestActorKindOriginContractFrozen is the compile-time config test for the
// additive actor contract (design §6 P2-15, DECISIONS #357): the legacy
// fields survive, the new Kind/Origin fields exist, and the constant values
// are frozen so no later edit can silently weaken a kind/origin check.
func TestActorKindOriginContractFrozen(t *testing.T) {
	actor := &Actor{
		UserID:    uuid.New(),
		SessionID: uuid.New(),
		IsAdmin:   true,
		Scopes:    []string{"admin"},
		Kind:      ActorKindCLI,
		Origin:    ActorOriginLocal,
	}
	if actor.UserID == uuid.Nil || actor.SessionID == uuid.Nil || !actor.IsAdmin || len(actor.Scopes) != 1 {
		t.Fatal("legacy actor fields must be retained verbatim")
	}
	if actor.Kind != ActorKindCLI || actor.Origin != ActorOriginLocal {
		t.Fatal("new actor fields must be present and assignable")
	}

	kinds := map[ActorKind]string{
		ActorKindUser:    "user",
		ActorKindSession: "session",
		ActorKindPAT:     "pat",
		ActorKindCLI:     "cli",
		ActorKindJob:     "job",
	}
	if len(kinds) != 5 {
		t.Fatalf("actor kinds = %d, want 5", len(kinds))
	}
	for kind, want := range kinds {
		if string(kind) != want {
			t.Errorf("ActorKind %q = %q, want %q", want, string(kind), want)
		}
	}

	origins := map[ActorOrigin]string{
		ActorOriginLocal:  "local",
		ActorOriginRemote: "remote",
	}
	if len(origins) != 2 {
		t.Fatalf("actor origins = %d, want 2", len(origins))
	}
	for origin, want := range origins {
		if string(origin) != want {
			t.Errorf("ActorOrigin %q = %q, want %q", want, string(origin), want)
		}
	}
}

// TestActorZeroValueNeverPassesLocalCLIChecks pins the fail-closed zero
// values: an actor constructed without Kind/Origin (a transitional caller)
// can never satisfy the local-CLI identity checks.
func TestActorZeroValueNeverPassesLocalCLIChecks(t *testing.T) {
	var zero Actor
	if zero.Kind == ActorKindCLI {
		t.Fatal("zero Kind must never equal ActorKindCLI")
	}
	if zero.Origin == ActorOriginLocal {
		t.Fatal("zero Origin must never equal ActorOriginLocal")
	}
	adminOnly := &Actor{UserID: uuid.New(), IsAdmin: true}
	if adminOnly.Kind == ActorKindCLI || adminOnly.Origin == ActorOriginLocal {
		t.Fatal("Kind/Origin must be set explicitly; zero values are not the local CLI identity")
	}
}
