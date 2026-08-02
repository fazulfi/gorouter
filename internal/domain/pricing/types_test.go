package pricing

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func stamped(providerID, modelID string, inputPrice, outputPrice float64) PriceOverride {
	return PriceOverride{
		ModelID:     modelID,
		ProviderID:  providerID,
		InputPrice:  inputPrice,
		OutputPrice: outputPrice,
		UpdatedBy:   uuid.New(),
		UpdatedAt:   time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC),
	}
}

func TestPriceOverride_Validate(t *testing.T) {
	valid := stamped("openai", "gpt-4o", 2.5, 10.0)
	cases := []struct {
		name      string
		mutate    func(*PriceOverride)
		wantError bool
	}{
		{"valid", func(*PriceOverride) {}, false},
		{"zero prices are a verified free tier", func(o *PriceOverride) { o.InputPrice, o.OutputPrice = 0, 0 }, false},
		{"missing model", func(o *PriceOverride) { o.ModelID = "" }, true},
		{"whitespace model", func(o *PriceOverride) { o.ModelID = "  " }, true},
		{"missing provider", func(o *PriceOverride) { o.ProviderID = "" }, true},
		{"negative input price", func(o *PriceOverride) { o.InputPrice = -0.01 }, true},
		{"negative output price", func(o *PriceOverride) { o.OutputPrice = -1 }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := valid
			c.mutate(&o)
			err := o.Validate()
			if c.wantError && err == nil {
				t.Errorf("Validate() = nil, want error")
			}
			if !c.wantError && err != nil {
				t.Errorf("Validate() = %v, want nil", err)
			}
		})
	}
}

func TestPriceOverride_Key(t *testing.T) {
	o := stamped("openai", "gpt-4o", 1, 2)
	if got := o.Key(); got != "openai/gpt-4o" {
		t.Errorf("Key() = %q", got)
	}
}

func TestSortOverrides_DeterministicOrder(t *testing.T) {
	shuffled := []PriceOverride{
		stamped("b-provider", "model-2", 1, 1),
		stamped("a-provider", "model-1", 1, 1),
		stamped("a-provider", "model-0", 1, 1),
	}
	SortOverrides(shuffled)
	want := []string{
		"a-provider/model-0",
		"a-provider/model-1",
		"b-provider/model-2",
	}
	for i, w := range want {
		if got := shuffled[i].Key(); got != w {
			t.Errorf("position %d = %q, want %q", i, got, w)
		}
	}
}

func TestPriceOverrideJSON_DeterministicEncoding(t *testing.T) {
	canonical := []PriceOverride{
		stamped("anthropic", "claude-3-5-sonnet", 3.0, 15.0),
		stamped("openai", "gpt-4o", 2.5, 10.0),
		stamped("openai", "gpt-4o-mini", 0.15, 0.6),
	}
	reversed := []PriceOverride{
		canonical[2], canonical[1], canonical[0],
	}

	SortOverrides(canonical)
	SortOverrides(reversed)

	a, err := json.Marshal(canonical)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	b, err := json.Marshal(reversed)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("encoding depends on insertion order:\n%s\n%s", a, b)
	}

	// Byte-stable across repeated encodings of the same set.
	c, _ := json.Marshal(canonical)
	if string(a) != string(c) {
		t.Errorf("encoding not stable across runs:\n%s\n%s", a, c)
	}

	var decoded []PriceOverride
	if err := json.Unmarshal(a, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded) != 3 {
		t.Fatalf("decoded = %d entries", len(decoded))
	}
	for i, d := range decoded {
		if d.Key() != canonical[i].Key() || d.InputPrice != canonical[i].InputPrice {
			t.Errorf("decoded[%d] = %+v", i, d)
		}
	}
}
