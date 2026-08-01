package routing

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/modelref"
	"gorouter/internal/domain/provider"
)

// ErrAliasNotFound is returned when an alias does not exist.
var ErrAliasNotFound = errAlias("alias not found")

// ErrAliasExists is returned when an alias name is already taken (schema
// UNIQUE(alias)).
var ErrAliasExists = errAlias("alias already exists")

// ErrInvalidAlias is returned when an alias name violates the grammar
// (decision #31: aliases are slashless).
var ErrInvalidAlias = errAlias("invalid alias name")

type errAlias string

func (e errAlias) Error() string { return string(e) }

// Alias mirrors the gorouter_model_aliases table exactly
// (000004_engine_matrix.up.sql): id, alias, target, provider_id, is_active,
// created_at, updated_at.
type Alias struct {
	ID         uuid.UUID
	Alias      string
	Target     string
	ProviderID *uuid.UUID
	IsActive   bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// AliasRepository defines persistence operations for model aliases.
type AliasRepository interface {
	Create(ctx context.Context, a *Alias) error
	Update(ctx context.Context, a *Alias) error
	Delete(ctx context.Context, id uuid.UUID) error
	FindByAlias(ctx context.Context, alias string) (*Alias, error)
	FindByID(ctx context.Context, id uuid.UUID) (*Alias, error)
	List(ctx context.Context) ([]Alias, error)
	ListActive(ctx context.Context) ([]Alias, error)
	SetActive(ctx context.Context, id uuid.UUID, active bool) error
}

// ValidateAlias checks that an alias name is non-empty and contains no "/" or
// ":" delimiter (decision #31: slashless aliases).
func ValidateAlias(name string) error {
	if name == "" || strings.ContainsAny(name, "/:") {
		return ErrInvalidAlias
	}
	return nil
}

// BuiltinAlias is a statically-defined model alias.
type BuiltinAlias struct {
	Alias  string
	Target string
}

// BuiltinModelAliases matches upstream BUILTIN_MODEL_ALIASES
// (open-sse/services/model.js:20-22).
var BuiltinModelAliases = []BuiltinAlias{
	{Alias: "grok-build", Target: "gcli/grok-build"},
}

// InferenceRule maps a model-name prefix to a provider.
type InferenceRule struct {
	Prefix   string
	Provider string
}

// ModelPrefixProviders matches upstream inferProviderFromModelName
// (open-sse/services/model.js:126-142, audit 06 §4.1):
// claude- → anthropic, gemini- → gemini, gpt- → openai, o[134] → openai,
// deepseek- → openrouter.
var ModelPrefixProviders = []InferenceRule{
	{Prefix: "claude-", Provider: "anthropic"},
	{Prefix: "gemini-", Provider: "gemini"},
	{Prefix: "gpt-", Provider: "openai"},
	{Prefix: "o1", Provider: "openai"},
	{Prefix: "o3", Provider: "openai"},
	{Prefix: "o4", Provider: "openai"},
	{Prefix: "deepseek-", Provider: "openrouter"},
}

// DefaultInferredProvider is the final fallback when no prefix rule matches
// (audit 06 §8: model.js:118-122).
const DefaultInferredProvider = "openai"

// InferProvider applies the audited prefix rules in original order and falls
// back to DefaultInferredProvider.
func InferProvider(model string) string {
	for _, rule := range ModelPrefixProviders {
		if strings.HasPrefix(model, rule.Prefix) {
			return rule.Provider
		}
	}
	return DefaultInferredProvider
}

// UpstreamProviderAliases is the audited provider-alias map subset
// (audit 06 §4.2, open-sse/providers/registry/index.js example):
// registry { id: "anthropic", alias: "claude", aliases: ["cc", "anthropic"] }
// maps id/alias/aliases to the canonical id. The full registry table is
// generated from provider-matrix.yaml in P3-T13.
var UpstreamProviderAliases = map[string]string{
	"anthropic": "anthropic",
	"claude":    "anthropic",
	"cc":        "anthropic",
}

// ProviderAliasSource resolves a provider segment (id/alias) to the canonical
// provider name, mirroring upstream resolveProviderAlias (model.js:27-29).
type ProviderAliasSource interface {
	ResolveProviderAlias(ctx context.Context, aliasOrID string) (string, bool, error)
}

// ProviderRepoAliasSource resolves provider aliases from the configured
// providers (by Name or Type, matching the Phase 2 app resolver) and falls
// back to the audited upstream alias map.
type ProviderRepoAliasSource struct {
	providers provider.ProviderRepository
}

// NewProviderRepoAliasSource creates a ProviderAliasSource backed by the
// provider repository plus the audited upstream alias map.
func NewProviderRepoAliasSource(providers provider.ProviderRepository) *ProviderRepoAliasSource {
	return &ProviderRepoAliasSource{providers: providers}
}

// ResolveProviderAlias looks the segment up in the configured providers first
// (config wins), then in the audited static map.
func (s *ProviderRepoAliasSource) ResolveProviderAlias(ctx context.Context, aliasOrID string) (string, bool, error) {
	if s.providers != nil {
		all, err := s.providers.List(ctx)
		if err != nil {
			return "", false, err
		}
		for i := range all {
			if all[i].Name == aliasOrID || string(all[i].Type) == aliasOrID {
				return all[i].Name, true, nil
			}
		}
	}
	canonical, ok := UpstreamProviderAliases[aliasOrID]
	return canonical, ok, nil
}

// ExtendedRef is a parsed model reference with the namespace/model(variant)
// extension applied (decisions #8, #31, #324). Original grammar fields come
// from modelref.ModelRef; Variant is the extension suffix.
type ExtendedRef struct {
	modelref.ModelRef
	Variant string
}

// ParseExtended parses a model reference with original grammar first
// (modelref.Parse) and then extracts the trailing (variant) suffix from the
// model segment. Unqualified references pass through untouched so alias
// lookup sees the verbatim string (original behavior preserved).
func ParseExtended(raw string) (ExtendedRef, error) {
	ref, err := modelref.Parse(raw)
	if err != nil {
		return ExtendedRef{}, err
	}
	ext := ExtendedRef{ModelRef: ref}
	if !ref.IsUnqualified() && ref.Model != "" {
		if base, variant, ok := splitVariant(ref.Model); ok {
			ext.Model = base
			ext.Variant = variant
		}
	}
	return ext, nil
}

// splitVariant extracts a well-formed trailing "(name)" suffix. Unmatched or
// malformed parens leave the model name literal.
func splitVariant(model string) (string, string, bool) {
	if !strings.HasSuffix(model, ")") {
		return model, "", false
	}
	idx := strings.LastIndex(model, "(")
	if idx <= 0 {
		return model, "", false
	}
	variant := model[idx+1 : len(model)-1]
	if variant == "" || strings.ContainsAny(variant, "()") {
		return model, "", false
	}
	return model[:idx], variant, true
}

// ResolvedModel is the outcome of alias-aware model resolution.
type ResolvedModel struct {
	Original   string
	Provider   string
	Model      string
	Variant    string
	ProviderID *uuid.UUID
	ViaAlias   string
	Builtin    bool
	Inferred   bool
}

// AliasResolver implements the upstream model resolution order
// (audit 06 §6.1 invariant 1, decisions #31/#98/#324):
//
//	provider/model (with slash) → provider alias lookup
//	no-slash → user alias DB → built-in aliases → prefix inference → "openai"
type AliasResolver struct {
	repo   AliasRepository
	source ProviderAliasSource
}

// NewAliasResolver creates an AliasResolver over the given alias repository
// and provider alias source.
func NewAliasResolver(repo AliasRepository, source ProviderAliasSource) *AliasResolver {
	return &AliasResolver{repo: repo, source: source}
}

// ResolveModel resolves a model string through the upstream resolution order.
func (r *AliasResolver) ResolveModel(ctx context.Context, modelStr string) (*ResolvedModel, error) {
	parsed, err := ParseExtended(modelStr)
	if err != nil {
		return nil, err
	}
	out := &ResolvedModel{Original: modelStr}

	if !parsed.IsUnqualified() {
		out.Provider = r.resolveProviderAlias(ctx, parsed.Provider)
		out.Model = parsed.Model
		out.Variant = parsed.Variant
		return out, nil
	}

	if alias, ok, err := r.lookupUserAlias(ctx, parsed.Raw); err != nil {
		return nil, err
	} else if ok {
		out.ProviderID = alias.ProviderID
		out.ViaAlias = alias.Alias
		return r.resolveAliasTarget(ctx, BuiltinAlias{Alias: alias.Alias, Target: alias.Target}, out)
	}

	if builtin, ok := lookupBuiltinAlias(parsed.Raw); ok {
		out.Builtin = true
		return r.resolveAliasTarget(ctx, builtin, out)
	}

	out.Provider = InferProvider(parsed.Raw)
	out.Model = parsed.Raw
	out.Inferred = true
	return out, nil
}

// resolveProviderAlias maps a provider segment to its canonical name,
// preserving unknown segments verbatim.
func (r *AliasResolver) resolveProviderAlias(ctx context.Context, segment string) string {
	if r.source != nil {
		if canonical, ok, err := r.source.ResolveProviderAlias(ctx, segment); err == nil && ok {
			return canonical
		}
	}
	return segment
}

// lookupUserAlias returns the active user alias for name, or nil.
func (r *AliasResolver) lookupUserAlias(ctx context.Context, name string) (*Alias, bool, error) {
	if r.repo == nil {
		return nil, false, nil
	}
	a, err := r.repo.FindByAlias(ctx, name)
	if err != nil {
		return nil, false, err
	}
	if a == nil || !a.IsActive {
		return nil, false, nil
	}
	return a, true, nil
}

// lookupBuiltinAlias searches the built-in alias table.
func lookupBuiltinAlias(name string) (BuiltinAlias, bool) {
	for _, b := range BuiltinModelAliases {
		if b.Alias == name {
			return b, true
		}
	}
	return BuiltinAlias{}, false
}

// resolveAliasTarget resolves an alias target. Slashed/colon targets parse as
// provider/model; bare targets go through inference. A provider-scoped alias
// (provider_id set) binds a bare target to that provider.
func (r *AliasResolver) resolveAliasTarget(ctx context.Context, b BuiltinAlias, out *ResolvedModel) (*ResolvedModel, error) {
	target := strings.TrimSpace(b.Target)
	if target == "" {
		return nil, ErrInvalidAlias
	}

	parsed, err := ParseExtended(target)
	if err == nil && !parsed.IsUnqualified() {
		out.Provider = r.resolveProviderAlias(ctx, parsed.Provider)
		out.Model = parsed.Model
		out.Variant = parsed.Variant
		return out, nil
	}

	out.Model = target
	if out.ProviderID == nil {
		out.Provider = InferProvider(target)
		out.Inferred = true
	}
	return out, nil
}
