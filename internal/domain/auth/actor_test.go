package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestContextWithActor_RoundTrip(t *testing.T) {
	actor := &Actor{
		UserID:    uuid.New(),
		SessionID: uuid.New(),
		IsAdmin:   true,
		Scopes:    []string{"admin", "proxy:read"},
	}

	ctx := ContextWithActor(context.Background(), actor)
	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("FromContext returned false after ContextWithActor")
	}
	if got == nil {
		t.Fatal("FromContext returned nil actor")
	}
	if got.UserID != actor.UserID {
		t.Errorf("expected UserID %v, got %v", actor.UserID, got.UserID)
	}
	if got.SessionID != actor.SessionID {
		t.Errorf("expected SessionID %v, got %v", actor.SessionID, got.SessionID)
	}
	if got.IsAdmin != actor.IsAdmin {
		t.Errorf("expected IsAdmin %v, got %v", actor.IsAdmin, got.IsAdmin)
	}
	if len(got.Scopes) != len(actor.Scopes) {
		t.Errorf("expected %d scopes, got %d", len(actor.Scopes), len(got.Scopes))
	}
}

func TestContextWithActor_OverwritesPrevious(t *testing.T) {
	actor1 := &Actor{UserID: uuid.New(), Scopes: []string{"read"}}
	actor2 := &Actor{UserID: uuid.New(), Scopes: []string{"admin"}}

	ctx := ContextWithActor(context.Background(), actor1)
	ctx = ContextWithActor(ctx, actor2)

	got, ok := FromContext(ctx)
	if !ok {
		t.Fatal("FromContext returned false")
	}
	if got.UserID != actor2.UserID {
		t.Errorf("expected UserID %v, got %v", actor2.UserID, got.UserID)
	}
}

func TestFromContext_MissingContext(t *testing.T) {
	_, ok := FromContext(context.Background())
	if ok {
		t.Fatal("FromContext should return false for context without actor")
	}
}

func TestFromContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), ActorKey, "not-an-actor")
	_, ok := FromContext(ctx)
	if ok {
		t.Fatal("FromContext should return false for wrong value type")
	}
}
