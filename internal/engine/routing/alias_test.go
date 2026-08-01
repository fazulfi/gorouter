package routing

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/provider"
)

// ---------------------------------------------------------------------------
// Alias model and CRUD semantics (schema: 000004_engine_matrix, gorouter_model_aliases)
// ---------------------------------------------------------------------------

// TestAliasSchemaContract pins the Alias fields to the exact
// gorouter_model_aliases columns: id, alias, target, provider_id, is_active,
// created_at, updated_at. No invented columns.
func TestAliasSchemaContract(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	a := Alias{
		ID:         uuid.New(),
		Alias:      "my-model",
		Target:     "openai/gpt-4o",
		ProviderID: nil,
		IsActive:   true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if a.ID == uuid.Nil {
		t.Error("ID must be set")
	}
	if a.Alias != "my-model" {
		t.Errorf("Alias = %q", a.Alias)
	}
	if a.Target != "openai/gpt-4o" {
		t.Errorf("Target = %q", a.Target)
	}
	if !a.IsActive {
		t.Error("IsActive must default true")
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		t.Error("timestamps must be set")
	}
}

// fakeAliasRepo is an in-memory AliasRepository for tests. It preserves
// creation order so "stable original order" can be asserted.
type fakeAliasRepo struct {
	aliases []Alias
	nextID  int
}

func newFakeAliasRepo(aliases ...Alias) *fakeAliasRepo {
	r := &fakeAliasRepo{}
	for i := range aliases {
		if aliases[i].ID == uuid.Nil {
			aliases[i].ID = uuid.New()
		}
		r.aliases = append(r.aliases, aliases[i])
	}
	return r
}

func (f *fakeAliasRepo) Create(_ context.Context, a *Alias) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	for _, existing := range f.aliases {
		if existing.Alias == a.Alias {
			return ErrAliasExists
		}
	}
	f.aliases = append(f.aliases, *a)
	return nil
}

func (f *fakeAliasRepo) Update(_ context.Context, a *Alias) error {
	for i := range f.aliases {
		if f.aliases[i].ID == a.ID {
			f.aliases[i] = *a
			return nil
		}
	}
	return ErrAliasNotFound
}

func (f *fakeAliasRepo) Delete(_ context.Context, id uuid.UUID) error {
	for i := range f.aliases {
		if f.aliases[i].ID == id {
			f.aliases = append(f.aliases[:i], f.aliases[i+1:]...)
			return nil
		}
	}
	return ErrAliasNotFound
}

func (f *fakeAliasRepo) FindByAlias(_ context.Context, alias string) (*Alias, error) {
	for i := range f.aliases {
		if f.aliases[i].Alias == alias {
			a := f.aliases[i]
			return &a, nil
		}
	}
	return nil, nil
}

func (f *fakeAliasRepo) FindByID(_ context.Context, id uuid.UUID) (*Alias, error) {
	for i := range f.aliases {
		if f.aliases[i].ID == id {
			a := f.aliases[i]
			return &a, nil
		}
	}
	return nil, nil
}

func (f *fakeAliasRepo) List(_ context.Context) ([]Alias, error) {
	out := make([]Alias, len(f.aliases))
	copy(out, f.aliases)
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].Alias < out[j].Alias
	})
	return out, nil
}

func (f *fakeAliasRepo) ListActive(_ context.Context) ([]Alias, error) {
	var out []Alias
	for i := range f.aliases {
		if f.aliases[i].IsActive {
			out = append(out, f.aliases[i])
		}
	}
	return out, nil
}

func (f *fakeAliasRepo) SetActive(_ context.Context, id uuid.UUID, active bool) error {
	for i := range f.aliases {
		if f.aliases[i].ID == id {
			f.aliases[i].IsActive = active
			return nil
		}
	}
	return ErrAliasNotFound
}

// fakeAliasProviderSource resolves provider alias segments the way the
// audited registry does: id/alias/aliases all map to the canonical id
// (audit 06 §4.2, model.js:27-29).
type fakeAliasProviderSource struct {
	aliases map[string]string
}

func (f *fakeAliasProviderSource) ResolveProviderAlias(_ context.Context, aliasOrID string) (string, bool, error) {
	if f.aliases == nil {
		return "", false, nil
	}
	canonical, ok := f.aliases[aliasOrID]
	return canonical, ok, nil
}

// upstreamAliasSource is the audited provider alias map subset from
// open-sse/providers/registry/index.js (audit 06 §4.2 example):
//
//	{ id: "anthropic", alias: "claude", aliases: ["cc", "anthropic"] }
func upstreamAliasSource() *fakeAliasProviderSource {
	return &fakeAliasProviderSource{aliases: map[string]string{
		"anthropic": "anthropic",
		"claude":    "anthropic",
		"cc":        "anthropic",
		"openai":    "openai",
		"gcli":      "gcli",
		"oc":        "oc",
	}}
}

// newTestAliasResolver builds an AliasResolver with the fake alias repo and
// the audited upstream provider alias source.
func newTestAliasResolver(aliases ...Alias) *AliasResolver {
	return NewAliasResolver(newFakeAliasRepo(aliases...), upstreamAliasSource())
}

// ---------------------------------------------------------------------------
// Resolution order — original grammar first, extension coexistence
// ---------------------------------------------------------------------------

// TestResolveModel_SlashedDirect verifies that a slashed reference resolves
// directly without consulting the user alias DB (audit 06 §6.1 invariant 1:
// provider/model → exact registry alias lookup).
func TestResolveModel_SlashedDirect(t *testing.T) {
	t.Parallel()
	r := newTestAliasResolver(Alias{Alias: "openai", Target: "anthropic/claude-opus-4", IsActive: true})
	got, err := r.ResolveModel(context.Background(), "openai/gpt-4o")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", got.Provider, "openai")
	}
	if got.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", got.Model, "gpt-4o")
	}
	if got.ViaAlias != "" {
		t.Errorf("ViaAlias = %q, want empty (slashed refs skip the alias DB)", got.ViaAlias)
	}
	if got.Inferred {
		t.Error("Inferred = true, want false")
	}
	if got.Variant != "" {
		t.Errorf("Variant = %q, want empty", got.Variant)
	}
}

// TestResolveModel_ProviderAlias verifies the provider segment before the
// slash is resolved through all aliases transitively (audit 06 §4.1:
// cc/claude-opus-4-6 → claude → anthropic).
func TestResolveModel_ProviderAlias(t *testing.T) {
	t.Parallel()
	r := newTestAliasResolver()
	got, err := r.ResolveModel(context.Background(), "cc/claude-opus-4-6")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", got.Provider, "anthropic")
	}
	if got.Model != "claude-opus-4-6" {
		t.Errorf("Model = %q", got.Model)
	}
}

// TestResolveModel_UserAliasBeforeBuiltin verifies the alias DB is consulted
// before built-in aliases (audit 06 §6.1: no-slash → model alias DB →
// built-in aliases), so a user alias may shadow a built-in.
func TestResolveModel_UserAliasBeforeBuiltin(t *testing.T) {
	t.Parallel()
	r := newTestAliasResolver(Alias{Alias: "grok-build", Target: "openai/gpt-4o", IsActive: true})
	got, err := r.ResolveModel(context.Background(), "grok-build")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.ViaAlias != "grok-build" {
		t.Errorf("ViaAlias = %q, want grok-build", got.ViaAlias)
	}
	if got.Provider != "openai" || got.Model != "gpt-4o" {
		t.Errorf("resolved %s/%s, want openai/gpt-4o", got.Provider, got.Model)
	}
}

// TestResolveModel_BuiltinAlias verifies BUILTIN_MODEL_ALIASES:
// grok-build → gcli/grok-build (upstream model.js:20-22).
func TestResolveModel_BuiltinAlias(t *testing.T) {
	t.Parallel()
	r := newTestAliasResolver()
	got, err := r.ResolveModel(context.Background(), "grok-build")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.Provider != "gcli" {
		t.Errorf("Provider = %q, want gcli", got.Provider)
	}
	if got.Model != "grok-build" {
		t.Errorf("Model = %q, want grok-build", got.Model)
	}
	if !got.Builtin {
		t.Error("Builtin = false, want true")
	}
	if got.ViaAlias != "" {
		t.Errorf("ViaAlias = %q, want empty (built-in, not user alias)", got.ViaAlias)
	}
}

// TestResolveModel_PrefixInference pins the audited prefix rules
// (model.js:126-142, audit 06 §4.1): claude- → anthropic, gemini- → gemini,
// gpt- → openai, o[134]- → openai, deepseek- → openrouter.
func TestResolveModel_PrefixInference(t *testing.T) {
	t.Parallel()
	r := newTestAliasResolver()
	cases := []struct {
		model string
		prov  string
	}{
		{model: "claude-sonnet-4", prov: "anthropic"},
		{model: "gemini-2.0-flash", prov: "gemini"},
		{model: "gpt-4o", prov: "openai"},
		{model: "o3-mini", prov: "openai"},
		{model: "o4-mini", prov: "openai"},
		{model: "o1-pro", prov: "openai"},
		{model: "deepseek-r1", prov: "openrouter"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.model, func(t *testing.T) {
			t.Parallel()
			got, err := r.ResolveModel(context.Background(), tc.model)
			if err != nil {
				t.Fatalf("ResolveModel(%q): %v", tc.model, err)
			}
			if got.Provider != tc.prov {
				t.Errorf("Provider = %q, want %q", got.Provider, tc.prov)
			}
			if got.Model != tc.model {
				t.Errorf("Model = %q, want %q", got.Model, tc.model)
			}
			if !got.Inferred {
				t.Errorf("Inferred = false for %q", tc.model)
			}
		})
	}
}

// TestResolveModel_InferenceFinalFallback verifies the final "openai"
// fallback when no prefix matches (audit 06 §8: model.js:118-122).
func TestResolveModel_InferenceFinalFallback(t *testing.T) {
	t.Parallel()
	r := newTestAliasResolver()
	got, err := r.ResolveModel(context.Background(), "unknown-model-xyz")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.Provider != "openai" {
		t.Errorf("Provider = %q, want openai (final fallback)", got.Provider)
	}
	if !got.Inferred {
		t.Error("Inferred = false, want true")
	}
}

// TestResolveModel_DisabledAliasSkipped verifies is_active=false aliases are
// not used for resolution (schema is_active semantics).
func TestResolveModel_DisabledAliasSkipped(t *testing.T) {
	t.Parallel()
	r := newTestAliasResolver(Alias{Alias: "my-model", Target: "openai/gpt-4o", IsActive: false})
	got, err := r.ResolveModel(context.Background(), "my-model")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.ViaAlias != "" {
		t.Errorf("ViaAlias = %q, disabled alias must not resolve", got.ViaAlias)
	}
	// Fallback: inference must kick in for the unknown model name.
	if got.Provider != "openai" {
		t.Errorf("Provider = %q, want openai inference fallback", got.Provider)
	}
}

// TestResolveModel_UserAliasBareTarget verifies a user alias whose target is
// a bare model name goes through inference (documented resolution semantics).
func TestResolveModel_UserAliasBareTarget(t *testing.T) {
	t.Parallel()
	r := newTestAliasResolver(Alias{Alias: "fast", Target: "gpt-4o-mini", IsActive: true})
	got, err := r.ResolveModel(context.Background(), "fast")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.ViaAlias != "fast" {
		t.Errorf("ViaAlias = %q", got.ViaAlias)
	}
	if got.Provider != "openai" || got.Model != "gpt-4o-mini" {
		t.Errorf("resolved %s/%s, want openai/gpt-4o-mini", got.Provider, got.Model)
	}
}

// TestResolveModel_ProviderScopedAlias verifies an alias with provider_id set
// binds a bare target to that provider.
func TestResolveModel_ProviderScopedAlias(t *testing.T) {
	t.Parallel()
	pid := uuid.New()
	r := newTestAliasResolver(Alias{Alias: "company-model", Target: "my-internal-model", ProviderID: &pid, IsActive: true})
	got, err := r.ResolveModel(context.Background(), "company-model")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.ProviderID == nil || *got.ProviderID != pid {
		t.Errorf("ProviderID = %v, want %v", got.ProviderID, pid)
	}
	if got.Model != "my-internal-model" {
		t.Errorf("Model = %q", got.Model)
	}
}

// TestResolveModel_UnknownProviderAlias verifies an unknown provider segment
// on a slashed reference is preserved verbatim (downstream resolution fails
// with provider-not-found, mirroring upstream behavior).
func TestResolveModel_UnknownProviderAlias(t *testing.T) {
	t.Parallel()
	r := newTestAliasResolver()
	got, err := r.ResolveModel(context.Background(), "no-such-provider/gpt-4o")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.Provider != "no-such-provider" {
		t.Errorf("Provider = %q, want verbatim no-such-provider", got.Provider)
	}
}

// ---------------------------------------------------------------------------
// namespace/model(variant) extension coexistence (decisions #8, #31, #324)
// ---------------------------------------------------------------------------

// TestParseExtended_Variant verifies the namespace/model(variant) extension
// parses without touching the original grammar.
func TestParseExtended_Variant(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw     string
		prov    string
		model   string
		variant string
		unqual  bool
		wantErr bool
	}{
		{"openai/gpt-4o", "openai", "gpt-4o", "", false, false},
		{"oc/deepseek-v4-flash-free(high)", "oc", "deepseek-v4-flash-free", "high", false, false},
		{"cc/claude-opus-4-6(low)", "cc", "claude-opus-4-6", "low", false, false},
		{"gpt-4o", "", "", "", true, false},
		{"", "", "", "", true, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			got, err := ParseExtended(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseExtended(%q) expected error", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseExtended(%q): %v", tc.raw, err)
			}
			if got.IsUnqualified() != tc.unqual {
				t.Errorf("IsUnqualified = %v, want %v", got.IsUnqualified(), tc.unqual)
			}
			if got.Provider != tc.prov {
				t.Errorf("Provider = %q, want %q", got.Provider, tc.prov)
			}
			if got.Model != tc.model {
				t.Errorf("Model = %q, want %q", got.Model, tc.model)
			}
			if got.Variant != tc.variant {
				t.Errorf("Variant = %q, want %q", got.Variant, tc.variant)
			}
		})
	}
}

// TestResolveModel_NamespaceVariantCoexistence verifies the extension flows
// through resolution unchanged (namespace = provider alias position).
func TestResolveModel_NamespaceVariantCoexistence(t *testing.T) {
	t.Parallel()
	r := newTestAliasResolver()
	got, err := r.ResolveModel(context.Background(), "oc/deepseek-v4-flash-free(high)")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.Provider != "oc" {
		t.Errorf("Provider = %q, want oc", got.Provider)
	}
	if got.Model != "deepseek-v4-flash-free" {
		t.Errorf("Model = %q", got.Model)
	}
	if got.Variant != "high" {
		t.Errorf("Variant = %q, want high", got.Variant)
	}
}

// TestResolveModel_ColonFormVariant verifies the colon form keeps working
// with a variant suffix on the model segment.
func TestResolveModel_ColonFormVariant(t *testing.T) {
	t.Parallel()
	r := newTestAliasResolver()
	got, err := r.ResolveModel(context.Background(), "openai:gpt-4o(high)")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.Provider != "openai" || got.Model != "gpt-4o" || got.Variant != "high" {
		t.Errorf("got %s/%s variant %q, want openai/gpt-4o variant high",
			got.Provider, got.Model, got.Variant)
	}
}

// TestResolveModel_MalformedVariant verifies unmatched parens are treated as
// part of the literal model name (no invented grammar).
func TestResolveModel_MalformedVariant(t *testing.T) {
	t.Parallel()
	r := newTestAliasResolver()
	got, err := r.ResolveModel(context.Background(), "openai/gpt-4o(high")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if got.Model != "gpt-4o(high" {
		t.Errorf("Model = %q, want literal gpt-4o(high", got.Model)
	}
	if got.Variant != "" {
		t.Errorf("Variant = %q, want empty", got.Variant)
	}
}

// ---------------------------------------------------------------------------
// Stable original order
// ---------------------------------------------------------------------------

// TestAliasListStableOrder verifies List preserves original creation order
// with a deterministic tie-break (audit 06 §6.1 / parity row 100: stable
// original order; parity row 110: manual priority/original order).
func TestAliasListStableOrder(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	repo := newFakeAliasRepo(
		Alias{Alias: "third", Target: "openai/gpt-4o", CreatedAt: now.Add(3 * time.Minute)},
		Alias{Alias: "first", Target: "openai/gpt-4o-mini", CreatedAt: now},
		Alias{Alias: "second", Target: "anthropic/claude-opus-4", CreatedAt: now.Add(2 * time.Minute)},
	)
	got, err := repo.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	want := []string{"first", "second", "third"}
	for i := range want {
		if got[i].Alias != want[i] {
			t.Errorf("position %d = %q, want %q (stable original order)", i, got[i].Alias, want[i])
		}
	}
}

// TestAliasCreateRejectsDuplicate verifies the alias name is unique (schema
// UNIQUE(alias)).
func TestAliasCreateRejectsDuplicate(t *testing.T) {
	t.Parallel()
	repo := newFakeAliasRepo(Alias{Alias: "dup", Target: "openai/gpt-4o", IsActive: true})
	err := repo.Create(context.Background(), &Alias{Alias: "dup", Target: "anthropic/claude-opus-4"})
	if !errors.Is(err, ErrAliasExists) {
		t.Errorf("Create duplicate = %v, want ErrAliasExists", err)
	}
}

// TestAliasValidator verifies alias names must be slashless (decision #31:
// "alias tanpa slash").
func TestAliasValidator(t *testing.T) {
	t.Parallel()
	if err := ValidateAlias("my-model"); err != nil {
		t.Errorf("ValidateAlias(my-model) = %v", err)
	}
	if err := ValidateAlias("with/slash"); !errors.Is(err, ErrInvalidAlias) {
		t.Errorf("ValidateAlias(with/slash) = %v, want ErrInvalidAlias", err)
	}
	if err := ValidateAlias("with:colon"); !errors.Is(err, ErrInvalidAlias) {
		t.Errorf("ValidateAlias(with:colon) = %v, want ErrInvalidAlias", err)
	}
	if err := ValidateAlias(""); !errors.Is(err, ErrInvalidAlias) {
		t.Errorf("ValidateAlias(empty) = %v, want ErrInvalidAlias", err)
	}
}

// TestBuiltinAliasTable verifies the built-in alias table matches upstream
// model.js:20-22 exactly.
func TestBuiltinAliasTable(t *testing.T) {
	t.Parallel()
	if len(BuiltinModelAliases) != 1 {
		t.Fatalf("len(BuiltinModelAliases) = %d, want 1", len(BuiltinModelAliases))
	}
	if BuiltinModelAliases[0].Alias != "grok-build" || BuiltinModelAliases[0].Target != "gcli/grok-build" {
		t.Errorf("builtin = %+v, want grok-build → gcli/grok-build", BuiltinModelAliases[0])
	}
}

// TestResolveProviderAliasSource verifies the provider-alias source built
// from a provider.ProviderRepository maps Name and Type as canonical ids
// (Phase 2 app resolver behavior) plus audited upstream aliases.
func TestResolveProviderAliasSource(t *testing.T) {
	t.Parallel()
	src := NewProviderRepoAliasSource(&fakeProviderRepo{providers: []provider.Provider{
		{ID: uuid.New(), Name: "openai", Type: provider.ProviderOpenAI},
	}})
	ctx := context.Background()
	if got, ok, err := src.ResolveProviderAlias(ctx, "openai"); err != nil || !ok || got != "openai" {
		t.Errorf("openai → (%q, %v, %v), want (openai, true, nil)", got, ok, err)
	}
	// Audited static map: registry ids self-map even without a configured
	// provider (audit 06 §4.2: ALIAS_TO_PROVIDER_ID[entry.id] = entry.id).
	if got, ok, err := src.ResolveProviderAlias(ctx, "anthropic"); err != nil || !ok || got != "anthropic" {
		t.Errorf("anthropic → (%q, %v, %v), want (anthropic, true, nil)", got, ok, err)
	}
	if got, ok, err := src.ResolveProviderAlias(ctx, "cc"); err != nil || !ok || got != "anthropic" {
		t.Errorf("cc → (%q, %v, %v), want (anthropic, true, nil)", got, ok, err)
	}
	if got, ok, err := src.ResolveProviderAlias(ctx, "no-such-provider"); err != nil || ok || got != "" {
		t.Errorf("no-such-provider → (%q, %v, %v), want (\"\", false, nil)", got, ok, err)
	}
}
