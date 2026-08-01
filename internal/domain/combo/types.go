package combo

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
)

// Strategy enumerates the supported combo routing strategies.
type Strategy string

const (
	StrategySequential Strategy = "sequential"
	StrategyRoundRobin Strategy = "roundrobin"
	StrategyAutoSwitch Strategy = "autoswitch"
	StrategyFusion     Strategy = "fusion"
)

// IsValid reports whether s is a known strategy.
func (s Strategy) IsValid() bool {
	switch s {
	case StrategySequential, StrategyRoundRobin, StrategyAutoSwitch, StrategyFusion:
		return true
	default:
		return false
	}
}

func (s Strategy) String() string { return string(s) }

// SequentialConfig carries sequential combo options. Reserved for future
// options; the empty struct keeps config parsing uniform across strategies.
type SequentialConfig struct{}

// RoundRobinConfig carries sticky round-robin options.
type RoundRobinConfig struct {
	// StickyCount is the number of consecutive requests served by one member
	// before the rotation advances. 0 or 1 means rotate on every request.
	StickyCount int `json:"sticky_count"`
}

// WithDefaults returns c with zero fields replaced by defaults.
func (c RoundRobinConfig) WithDefaults() RoundRobinConfig {
	if c.StickyCount < 1 {
		c.StickyCount = 1
	}
	return c
}

// AutoSwitchConfig carries capability auto-switch options. Reserved for future
// options; capability selection is driven by the request.
type AutoSwitchConfig struct{}

// FusionConfig carries fusion panel, quorum, grace, timeout, and judge options.
// Durations are stored as Go duration strings (e.g. "8s") so JSONB configs stay
// human readable and parseable with time.ParseDuration.
type FusionConfig struct {
	// Quorum is the minimum number of successful panels required before the
	// judge synthesizes a result. Default: 2.
	Quorum int `json:"quorum"`

	// GracePeriod is the wait after the first panel success for more panels to
	// reach quorum. Default: 8s.
	GracePeriod string `json:"grace_period"`

	// HardTimeout is the absolute deadline for all panel execution and judge
	// synthesis. Default: 90s.
	HardTimeout string `json:"hard_timeout"`

	// JudgeModelID is the model reference (provider/model) used for judge
	// synthesis. It must resolve to an actually configured model at execution
	// time; execution fails before any panel fan-out otherwise.
	JudgeModelID string `json:"judge_model_id"`
}

// DefaultFusionConfig returns the upstream-compatible defaults.
func DefaultFusionConfig() FusionConfig {
	return FusionConfig{
		Quorum:      2,
		GracePeriod: "8s",
		HardTimeout: "90s",
	}
}

// WithDefaults returns c with zero fields replaced by defaults.
func (c FusionConfig) WithDefaults() FusionConfig {
	d := DefaultFusionConfig()
	if c.Quorum < 1 {
		c.Quorum = d.Quorum
	}
	if c.GracePeriod == "" {
		c.GracePeriod = d.GracePeriod
	}
	if c.HardTimeout == "" {
		c.HardTimeout = d.HardTimeout
	}
	return c
}

// EffectiveQuorum returns the quorum, defaulting to 2.
func (c FusionConfig) EffectiveQuorum() int { return c.WithDefaults().Quorum }

// Grace returns the parsed grace period.
func (c FusionConfig) Grace() (time.Duration, error) {
	c = c.WithDefaults()
	d, err := time.ParseDuration(c.GracePeriod)
	if err != nil {
		return 0, err
	}
	return d, nil
}

// Timeout returns the parsed hard timeout.
func (c FusionConfig) Timeout() (time.Duration, error) {
	c = c.WithDefaults()
	d, err := time.ParseDuration(c.HardTimeout)
	if err != nil {
		return 0, err
	}
	return d, nil
}

// Validate checks fusion invariants: positive quorum, parseable durations,
// grace shorter than the hard timeout, and a configured judge model.
func (c FusionConfig) Validate() error {
	c = c.WithDefaults()
	if c.Quorum < 1 {
		return errors.New("quorum must be at least 1")
	}
	grace, err := c.Grace()
	if err != nil {
		return errors.New("invalid grace_period: " + err.Error())
	}
	hard, err := c.Timeout()
	if err != nil {
		return errors.New("invalid hard_timeout: " + err.Error())
	}
	if grace <= 0 {
		return errors.New("grace_period must be positive")
	}
	if hard <= 0 {
		return errors.New("hard_timeout must be positive")
	}
	if grace >= hard {
		return errors.New("grace_period must be shorter than hard_timeout")
	}
	if c.JudgeModelID == "" {
		return errors.New("judge_model_id is required for fusion")
	}
	return nil
}

// Definition is a combo routing definition persisted in
// gorouter_combo_definitions.
type Definition struct {
	ID        uuid.UUID
	Name      string
	Strategy  Strategy
	Config    json.RawMessage
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FusionConfig decodes and validates the definition config as a fusion config.
func (d *Definition) FusionConfig() (FusionConfig, error) {
	var cfg FusionConfig
	if len(d.Config) == 0 {
		return DefaultFusionConfig(), nil
	}
	if err := json.Unmarshal(d.Config, &cfg); err != nil {
		return FusionConfig{}, err
	}
	return cfg.WithDefaults(), nil
}

// RoundRobinConfig decodes the definition config as a round-robin config.
func (d *Definition) RoundRobinConfig() (RoundRobinConfig, error) {
	var cfg RoundRobinConfig
	if len(d.Config) == 0 {
		return cfg.WithDefaults(), nil
	}
	if err := json.Unmarshal(d.Config, &cfg); err != nil {
		return RoundRobinConfig{}, err
	}
	return cfg.WithDefaults(), nil
}

// Member is one route (provider/account/model) inside a combo, persisted in
// gorouter_combo_members.
type Member struct {
	ID         uuid.UUID
	ComboID    uuid.UUID
	ProviderID uuid.UUID
	AccountID  *uuid.UUID
	ModelRef   string
	Priority   int
	Weight     int
	IsActive   bool
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Valid reports whether the member has the required routing fields.
func (m Member) Valid() bool {
	return m.ProviderID != uuid.Nil && m.ModelRef != ""
}

// SortMembers sorts members deterministically by execution order: priority
// ascending, weight descending, creation ascending, then ID.
func SortMembers(members []Member) {
	sort.SliceStable(members, func(i, j int) bool {
		a, b := members[i], members[j]
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if a.Weight != b.Weight {
			return a.Weight > b.Weight
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID.String() < b.ID.String()
	})
}

// ActiveMembers returns the enabled members in deterministic order.
func ActiveMembers(members []Member) []Member {
	out := make([]Member, 0, len(members))
	for _, m := range members {
		if m.IsActive {
			out = append(out, m)
		}
	}
	return out
}

// Repository is the persistence contract for combo definitions and members,
// backed by the P3-T06 migration tables gorouter_combo_definitions and
// gorouter_combo_members.
type Repository interface {
	Create(ctx context.Context, d *Definition) error
	FindByID(ctx context.Context, id uuid.UUID) (*Definition, error)
	FindByName(ctx context.Context, name string) (*Definition, error)
	List(ctx context.Context) ([]Definition, error)
	Update(ctx context.Context, d *Definition) error
	Delete(ctx context.Context, id uuid.UUID) error
	SetActive(ctx context.Context, id uuid.UUID, active bool) error

	ListMembers(ctx context.Context, comboID uuid.UUID) ([]Member, error)
	UpsertMembers(ctx context.Context, comboID uuid.UUID, members []Member) error
	DeleteMember(ctx context.Context, memberID uuid.UUID) error
}

// RuntimeState is the approved runtime/application checkpoint storage used by
// combo strategies (round-robin stickiness), backed by gorouter_runtime_state.
// No new tables are introduced; rotation state survives restart through this
// key-value checkpoint.
type RuntimeState interface {
	Get(ctx context.Context, key string) (json.RawMessage, error)
	Set(ctx context.Context, key string, value json.RawMessage, ttl time.Duration) error
}
