// Package combos provides the application-layer combo service: definition and
// member persistence, plus execution of the four combo strategies through the
// engine layer.
package combos

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorouter/internal/domain/combo"
	enginecombos "gorouter/internal/engine/combos"

	"github.com/google/uuid"
)

// CapabilityChecker reports whether a member's model satisfies all requested
// capabilities. The production wiring resolves member models through the
// provider model catalog.
type CapabilityChecker func(ctx context.Context, member combo.Member, capabilities []string) (bool, error)

// JudgeResolver resolves a judge model reference to an actually configured
// model member. It must fail when the model is not configured; execution then
// fails before any fusion panel fan-out.
type JudgeResolver func(ctx context.Context, judgeModelID string) (*combo.Member, error)

// ExecutionResult is the single terminal outcome of a combo execution.
type ExecutionResult struct {
	Definition combo.Definition
	Member     combo.Member
	Body       []byte
	JudgeUsed  bool
}

// Service orchestrates combo persistence and execution.
type Service struct {
	repo         combo.Repository
	state        combo.RuntimeState
	invoke       enginecombos.Invoker
	checkCaps    CapabilityChecker
	resolveJudge JudgeResolver
	sequential   *enginecombos.SequentialEngine
	roundRobin   *enginecombos.RoundRobinEngine
	autoSwitch   *enginecombos.AutoSwitchEngine
	fusion       *enginecombos.FusionEngine
}

// NewService creates the combo service. invoke, checkCaps, and resolveJudge
// must be wired to the executor/catalog adapters; tests inject fakes.
func NewService(repo combo.Repository, state combo.RuntimeState, invoke enginecombos.Invoker, checkCaps CapabilityChecker, resolveJudge JudgeResolver) *Service {
	return &Service{
		repo:         repo,
		state:        state,
		invoke:       invoke,
		checkCaps:    checkCaps,
		resolveJudge: resolveJudge,
		sequential:   enginecombos.NewSequentialEngine(),
		roundRobin:   enginecombos.NewRoundRobinEngine(&stickyStoreAdapter{state: state}),
		autoSwitch:   enginecombos.NewAutoSwitchEngine(),
		fusion:       enginecombos.NewFusionEngine(),
	}
}

// rotationTTL bounds how long rotation checkpoints live in runtime state.
const rotationTTL = 24 * time.Hour

func rotationKey(comboID uuid.UUID) string { return "combo:rotation:" + comboID.String() }

type stickyStoreAdapter struct{ state combo.RuntimeState }

func (a *stickyStoreAdapter) GetRotation(ctx context.Context, comboID uuid.UUID) (*enginecombos.Rotation, error) {
	raw, err := a.state.Get(ctx, rotationKey(comboID))
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, nil
	}
	var r enginecombos.Rotation
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("combos: decode rotation: %w", err)
	}
	return &r, nil
}

func (a *stickyStoreAdapter) SaveRotation(ctx context.Context, comboID uuid.UUID, r enginecombos.Rotation) error {
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return a.state.Set(ctx, rotationKey(comboID), raw, rotationTTL)
}

type catalogAdapter struct{ check CapabilityChecker }

func (a catalogAdapter) Supports(ctx context.Context, member combo.Member, capabilities []string) (bool, error) {
	return a.check(ctx, member, capabilities)
}

// Create validates and persists a combo definition with its members.
func (s *Service) Create(ctx context.Context, name string, strategy combo.Strategy, config json.RawMessage, members []combo.Member) (*combo.Definition, error) {
	if err := validateDefinition(name, strategy, config); err != nil {
		return nil, err
	}
	if err := validateMembers(members); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	def := &combo.Definition{
		ID:        uuid.New(),
		Name:      name,
		Strategy:  strategy,
		Config:    normalizeConfig(config),
		IsActive:  true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := s.repo.Create(ctx, def); err != nil {
		return nil, err
	}
	if err := s.repo.UpsertMembers(ctx, def.ID, stampMembers(def.ID, members, now)); err != nil {
		return nil, err
	}
	return def, nil
}

// Get returns the definition by ID.
func (s *Service) Get(ctx context.Context, id uuid.UUID) (*combo.Definition, error) {
	def, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, combo.ErrNotFound
	}
	return def, nil
}

// List returns all definitions.
func (s *Service) List(ctx context.Context) ([]combo.Definition, error) {
	return s.repo.List(ctx)
}

// Update validates and persists definition changes.
func (s *Service) Update(ctx context.Context, def *combo.Definition) error {
	if def == nil {
		return fmt.Errorf("combos: nil definition")
	}
	if err := validateDefinition(def.Name, def.Strategy, def.Config); err != nil {
		return err
	}
	def.UpdatedAt = time.Now().UTC()
	return s.repo.Update(ctx, def)
}

// Delete removes a definition and its members.
func (s *Service) Delete(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

// SetActive enables or disables a definition.
func (s *Service) SetActive(ctx context.Context, id uuid.UUID, active bool) error {
	return s.repo.SetActive(ctx, id, active)
}

// UpdateMembers replaces the member set of a definition.
func (s *Service) UpdateMembers(ctx context.Context, comboID uuid.UUID, members []combo.Member) error {
	if err := validateMembers(members); err != nil {
		return err
	}
	now := time.Now().UTC()
	return s.repo.UpsertMembers(ctx, comboID, stampMembers(comboID, members, now))
}

// Execute runs the definition's strategy with the request body and returns the
// single terminal outcome. capabilities only affect capability auto-switch.
func (s *Service) Execute(ctx context.Context, id uuid.UUID, request []byte, capabilities []string) (*ExecutionResult, error) {
	def, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if def == nil {
		return nil, combo.ErrNotFound
	}
	if !def.IsActive {
		return nil, combo.ErrInactive
	}
	members, err := s.repo.ListMembers(ctx, id)
	if err != nil {
		return nil, err
	}
	combo.SortMembers(members)

	switch def.Strategy {
	case combo.StrategySequential:
		return s.executeSequential(ctx, def, members, request)
	case combo.StrategyRoundRobin:
		return s.executeRoundRobin(ctx, def, members, request)
	case combo.StrategyAutoSwitch:
		return s.executeAutoSwitch(ctx, def, members, request, capabilities)
	case combo.StrategyFusion:
		return s.executeFusion(ctx, def, members, request)
	default:
		return nil, combo.ErrUnknownStrategy
	}
}

func (s *Service) executeSequential(ctx context.Context, def *combo.Definition, members []combo.Member, request []byte) (*ExecutionResult, error) {
	res, err := s.sequential.Run(ctx, members, request, s.invoke, enginecombos.DefaultFailureClassifier)
	if err != nil {
		return nil, err
	}
	return &ExecutionResult{Definition: *def, Member: res.Member, Body: res.Body}, nil
}

func (s *Service) executeRoundRobin(ctx context.Context, def *combo.Definition, members []combo.Member, request []byte) (*ExecutionResult, error) {
	cfg, err := def.RoundRobinConfig()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", combo.ErrInvalidConfig, err)
	}
	m, err := s.roundRobin.Select(ctx, def.ID, members, cfg.StickyCount)
	if err != nil {
		return nil, err
	}
	body, err := s.invoke(ctx, *m, request)
	if err != nil {
		return nil, err
	}
	return &ExecutionResult{Definition: *def, Member: *m, Body: body}, nil
}

func (s *Service) executeAutoSwitch(ctx context.Context, def *combo.Definition, members []combo.Member, request []byte, capabilities []string) (*ExecutionResult, error) {
	m, err := s.autoSwitch.Select(ctx, members, capabilities, catalogAdapter{check: s.checkCaps})
	if err != nil {
		return nil, err
	}
	body, err := s.invoke(ctx, *m, request)
	if err != nil {
		return nil, err
	}
	return &ExecutionResult{Definition: *def, Member: *m, Body: body}, nil
}

func (s *Service) executeFusion(ctx context.Context, def *combo.Definition, members []combo.Member, request []byte) (*ExecutionResult, error) {
	cfg, err := def.FusionConfig()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", combo.ErrInvalidConfig, err)
	}
	judgeMember, err := s.resolveJudge(ctx, cfg.JudgeModelID)
	if err != nil {
		return nil, fmt.Errorf("%w: judge %q: %v", combo.ErrJudgeModelUnresolved, cfg.JudgeModelID, err)
	}
	judge := func(ctx context.Context, panels []enginecombos.PanelResult) ([]byte, error) {
		return s.invokeJudge(ctx, *judgeMember, panels)
	}
	res, err := s.fusion.Run(ctx, cfg, members, request, s.invoke, judge)
	if err != nil {
		return nil, err
	}
	out := ExecutionResult{Definition: *def, Body: res.Body}
	if res.Synthesized {
		out.Member = *judgeMember
		out.JudgeUsed = true
	} else {
		out.Member = res.Member
	}
	return &out, nil
}

func (s *Service) invokeJudge(ctx context.Context, judgeMember combo.Member, panels []enginecombos.PanelResult) ([]byte, error) {
	type panelJSON struct {
		ModelRef string `json:"model_ref"`
		Body     string `json:"body,omitempty"`
		Error    string `json:"error,omitempty"`
	}
	body := struct {
		JudgeModelID string      `json:"judge_model_id"`
		Panels       []panelJSON `json:"panels"`
	}{
		JudgeModelID: judgeMember.ModelRef,
		Panels:       make([]panelJSON, 0, len(panels)),
	}
	for _, p := range panels {
		entry := panelJSON{ModelRef: p.Member.ModelRef, Body: string(p.Body)}
		if p.Err != nil {
			entry.Error = p.Err.Error()
		}
		body.Panels = append(body.Panels, entry)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return s.invoke(ctx, judgeMember, raw)
}

func validateDefinition(name string, strategy combo.Strategy, config json.RawMessage) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("combos: name is required")
	}
	if !strategy.IsValid() {
		return combo.ErrUnknownStrategy
	}
	switch strategy {
	case combo.StrategyFusion:
		var cfg combo.FusionConfig
		if len(config) > 0 {
			if err := json.Unmarshal(config, &cfg); err != nil {
				return fmt.Errorf("%w: %v", combo.ErrInvalidConfig, err)
			}
		}
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("%w: %v", combo.ErrInvalidConfig, err)
		}
	case combo.StrategyRoundRobin:
		var cfg combo.RoundRobinConfig
		if len(config) > 0 {
			if err := json.Unmarshal(config, &cfg); err != nil {
				return fmt.Errorf("%w: %v", combo.ErrInvalidConfig, err)
			}
		}
	}
	return nil
}

func validateMembers(members []combo.Member) error {
	if len(members) == 0 {
		return combo.ErrNoMembers
	}
	for _, m := range members {
		if !m.Valid() {
			return fmt.Errorf("%w: %+v", combo.ErrInvalidMember, m)
		}
	}
	return nil
}

func normalizeConfig(config json.RawMessage) json.RawMessage {
	if len(config) == 0 {
		return json.RawMessage(`{}`)
	}
	return config
}

func stampMembers(comboID uuid.UUID, members []combo.Member, now time.Time) []combo.Member {
	out := make([]combo.Member, len(members))
	for i, m := range members {
		if m.ID == uuid.Nil {
			m.ID = uuid.New()
		}
		m.ComboID = comboID
		m.CreatedAt = now
		m.UpdatedAt = now
		out[i] = m
	}
	return out
}
