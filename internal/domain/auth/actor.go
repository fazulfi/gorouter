package auth

import "context"

// ContextWithActor stores the given Actor in the context and returns the new
// context value. Use FromContext to retrieve it.
func ContextWithActor(ctx context.Context, actor *Actor) context.Context {
	return context.WithValue(ctx, ActorKey, actor)
}
