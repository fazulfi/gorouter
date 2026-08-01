package harness

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"gorouter/internal/domain/engine"
)

// CompareArtifacts compares a produced artifact against a golden
// fixture using JSON semantic equality (insensitive to key order and
// whitespace). It returns a descriptive error pinpointing the first
// differing path when the artifacts do not match.
func CompareArtifacts(got, want []byte) error {
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		return fmt.Errorf("artifact is not valid JSON: %w", err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		return fmt.Errorf("golden fixture is not valid JSON: %w", err)
	}
	if reflect.DeepEqual(g, w) {
		return nil
	}
	path, gv, wv := firstDiff(g, w)
	return fmt.Errorf("artifact mismatch at %s: got %s, want %s", path, clip(gv), clip(wv))
}

func clip(v string) string {
	if len(v) > 60 {
		return v[:60] + "..."
	}
	return v
}

func firstDiff(g, w any) (path, gv, wv string) {
	if reflect.TypeOf(g) != reflect.TypeOf(w) {
		return "<root>", fmt.Sprintf("%T", g), fmt.Sprintf("%T", w)
	}
	switch gt := g.(type) {
	case map[string]any:
		wt := w.(map[string]any)
		for k, wval := range wt {
			gval, ok := gt[k]
			if !ok {
				return "<root>." + k, "<absent>", fmt.Sprintf("%v", wval)
			}
			child := "<root>." + k
			if !reflect.DeepEqual(gval, wval) {
				p, gv, wv := firstDiff(gval, wval)
				if p != "" {
					child += strings.TrimPrefix(p, "<root>")
					return child, gv, wv
				}
				return child, fmt.Sprintf("%v", gval), fmt.Sprintf("%v", wval)
			}
		}
		for k, gval := range gt {
			if _, ok := wt[k]; !ok {
				return "<root>." + k, fmt.Sprintf("%v", gval), "<absent>"
			}
		}
	case []any:
		wt := w.([]any)
		if len(gt) != len(wt) {
			return "<root>", fmt.Sprintf("len %d", len(gt)), fmt.Sprintf("len %d", len(wt))
		}
		for i := range gt {
			if !reflect.DeepEqual(gt[i], wt[i]) {
				child := fmt.Sprintf("<root>[%d]", i)
				p, gv, wv := firstDiff(gt[i], wt[i])
				if p != "" {
					child += strings.TrimPrefix(p, "<root>")
					return child, gv, wv
				}
				return child, fmt.Sprintf("%v", gt[i]), fmt.Sprintf("%v", wt[i])
			}
		}
	default:
		return "<root>", fmt.Sprintf("%v", g), fmt.Sprintf("%v", w)
	}
	return "<root>", fmt.Sprintf("%v", g), fmt.Sprintf("%v", w)
}

// ValidateArtifactShape performs the live-mode structural validation
// of a provider artifact: the response must be a JSON object carrying
// the envelope expected for the format (choices/output/content/
// candidates) or a structured error object.
func ValidateArtifactShape(format engine.RequestFormat, body []byte) error {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("artifact is not a JSON object: %w", err)
	}
	if e, ok := doc["error"]; ok && e != nil {
		return nil
	}
	var envelope, kind string
	switch format {
	case engine.FormatOpenAIChat, engine.FormatOpenAICompat:
		envelope, kind = "choices", "array"
	case engine.FormatCodexResponses:
		envelope, kind = "output", "array"
	case engine.FormatAnthropic:
		envelope, kind = "content", "array"
	case engine.FormatGemini:
		envelope, kind = "candidates", "array"
	default:
		return fmt.Errorf("unknown format %s", format)
	}
	arr, ok := doc[envelope].([]any)
	if !ok || len(arr) == 0 {
		return fmt.Errorf("artifact for %s must carry a non-empty %q %s", format, envelope, kind)
	}
	return nil
}
