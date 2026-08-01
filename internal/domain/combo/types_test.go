package combo

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestStrategyIsValid(t *testing.T) {
	valid := []Strategy{StrategySequential, StrategyRoundRobin, StrategyAutoSwitch, StrategyFusion}
	for _, s := range valid {
		if !s.IsValid() {
			t.Errorf("expected %q to be valid", s)
		}
		if s.String() != string(s) {
			t.Errorf("String() mismatch for %q", s)
		}
	}
	for _, s := range []Strategy{"", "sticky", "FUSION", "random"} {
		if s.IsValid() {
			t.Errorf("expected %q to be invalid", s)
		}
	}
}

func TestFusionConfigDefaults(t *testing.T) {
	var zero FusionConfig
	cfg := zero.WithDefaults()
	if cfg.Quorum != 2 {
		t.Errorf("quorum default = %d, want 2", cfg.Quorum)
	}
	if cfg.GracePeriod != "8s" {
		t.Errorf("grace default = %q, want 8s", cfg.GracePeriod)
	}
	if cfg.HardTimeout != "90s" {
		t.Errorf("hard timeout default = %q, want 90s", cfg.HardTimeout)
	}
	grace, err := cfg.Grace()
	if err != nil || grace != 8*time.Second {
		t.Errorf("grace = %v, %v; want 8s", grace, err)
	}
	hard, err := cfg.Timeout()
	if err != nil || hard != 90*time.Second {
		t.Errorf("hard = %v, %v; want 90s", hard, err)
	}
	if cfg.EffectiveQuorum() != 2 {
		t.Errorf("EffectiveQuorum = %d, want 2", cfg.EffectiveQuorum())
	}
}

func TestFusionConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     FusionConfig
		wantErr bool
	}{
		{"defaults missing judge model", FusionConfig{}, true},
		{"valid", FusionConfig{Quorum: 2, GracePeriod: "8s", HardTimeout: "90s", JudgeModelID: "openai/gpt-4o"}, false},
		{"quorum zero defaults to 2", FusionConfig{Quorum: 0, GracePeriod: "8s", HardTimeout: "90s", JudgeModelID: "openai/gpt-4o"}, false},
		{"bad grace", FusionConfig{Quorum: 2, GracePeriod: "banana", HardTimeout: "90s", JudgeModelID: "openai/gpt-4o"}, true},
		{"bad hard", FusionConfig{Quorum: 2, GracePeriod: "8s", HardTimeout: "x", JudgeModelID: "openai/gpt-4o"}, true},
		{"grace equals hard", FusionConfig{Quorum: 2, GracePeriod: "90s", HardTimeout: "90s", JudgeModelID: "openai/gpt-4o"}, true},
		{"grace exceeds hard", FusionConfig{Quorum: 2, GracePeriod: "120s", HardTimeout: "90s", JudgeModelID: "openai/gpt-4o"}, true},
		{"zero grace", FusionConfig{Quorum: 2, GracePeriod: "0s", HardTimeout: "90s", JudgeModelID: "openai/gpt-4o"}, true},
		{"empty judge", FusionConfig{Quorum: 2, GracePeriod: "8s", HardTimeout: "90s"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefinitionConfigParsing(t *testing.T) {
	raw := json.RawMessage(`{"quorum":3,"grace_period":"5s","hard_timeout":"60s","judge_model_id":"anthropic/claude"}`)
	def := &Definition{ID: uuid.New(), Name: "f", Strategy: StrategyFusion, Config: raw}
	cfg, err := def.FusionConfig()
	if err != nil {
		t.Fatalf("FusionConfig: %v", err)
	}
	if cfg.Quorum != 3 || cfg.GracePeriod != "5s" || cfg.HardTimeout != "60s" || cfg.JudgeModelID != "anthropic/claude" {
		t.Errorf("parsed config mismatch: %+v", cfg)
	}

	empty := &Definition{Strategy: StrategyFusion}
	cfg, err = empty.FusionConfig()
	if err != nil {
		t.Fatalf("empty FusionConfig: %v", err)
	}
	if cfg.Quorum != 2 || cfg.GracePeriod != "8s" || cfg.HardTimeout != "90s" {
		t.Errorf("empty config should default: %+v", cfg)
	}

	bad := &Definition{Strategy: StrategyFusion, Config: json.RawMessage(`{not json`)}
	if _, err := bad.FusionConfig(); err == nil {
		t.Error("expected error for malformed config")
	}

	rrRaw := json.RawMessage(`{"sticky_count":4}`)
	rr := &Definition{Strategy: StrategyRoundRobin, Config: rrRaw}
	rrc, err := rr.RoundRobinConfig()
	if err != nil || rrc.StickyCount != 4 {
		t.Errorf("roundrobin config = %+v, %v", rrc, err)
	}
	rrc, err = (&Definition{Strategy: StrategyRoundRobin}).RoundRobinConfig()
	if err != nil || rrc.StickyCount != 1 {
		t.Errorf("roundrobin default = %+v, %v", rrc, err)
	}
}

func TestMemberValidation(t *testing.T) {
	good := Member{ProviderID: uuid.New(), ModelRef: "openai/gpt-4o"}
	if !good.Valid() {
		t.Error("expected valid member")
	}
	noProvider := Member{ModelRef: "openai/gpt-4o"}
	if noProvider.Valid() {
		t.Error("member without provider must be invalid")
	}
	noModel := Member{ProviderID: uuid.New()}
	if noModel.Valid() {
		t.Error("member without model must be invalid")
	}
}

func TestSortMembersDeterministic(t *testing.T) {
	c1 := time.Now().UTC()
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	members := []Member{
		{ID: ids[0], ComboID: uuid.New(), ProviderID: uuid.New(), ModelRef: "a", Priority: 2, Weight: 1, IsActive: true, CreatedAt: c1},
		{ID: ids[1], ComboID: uuid.New(), ProviderID: uuid.New(), ModelRef: "b", Priority: 1, Weight: 5, IsActive: true, CreatedAt: c1},
		{ID: ids[2], ComboID: uuid.New(), ProviderID: uuid.New(), ModelRef: "c", Priority: 1, Weight: 5, IsActive: true, CreatedAt: c1},
	}
	SortMembers(members)
	if members[2].ModelRef != "a" {
		t.Errorf("priority order wrong: %s,%s,%s; want ?,?,a", members[0].ModelRef, members[1].ModelRef, members[2].ModelRef)
	}
	if !(members[0].ID.String() < members[1].ID.String()) {
		t.Errorf("tie must sort by ID ascending: %s before %s", members[1].ID, members[0].ID)
	}
	first := []string{members[0].ModelRef, members[1].ModelRef, members[2].ModelRef}
	SortMembers(members)
	second := []string{members[0].ModelRef, members[1].ModelRef, members[2].ModelRef}
	if first[0] != second[0] || first[1] != second[1] || first[2] != second[2] {
		t.Error("sort must be stable across runs")
	}
}

func TestActiveMembers(t *testing.T) {
	in := []Member{
		{ID: uuid.New(), IsActive: false},
		{ID: uuid.New(), IsActive: true},
	}
	out := ActiveMembers(in)
	if len(out) != 1 || !out[0].IsActive {
		t.Errorf("ActiveMembers = %+v", out)
	}
}

func TestSentinelErrors(t *testing.T) {
	for _, err := range []error{ErrNotFound, ErrInactive, ErrUnknownStrategy, ErrInvalidConfig, ErrNoMembers, ErrInvalidMember, ErrJudgeModelUnresolved} {
		if err == nil {
			t.Error("sentinel error must not be nil")
		}
	}
	if !errors.Is(ErrNotFound, ErrNotFound) {
		t.Error("ErrNotFound must wrap itself")
	}
}
