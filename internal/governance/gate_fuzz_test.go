package governance

import (
	"encoding/json"
	"testing"
	"time"
)

// FuzzValidateGateEvidence fuzzes the ValidateGateEvidence function with
// randomly constructed GateEvidence structs to ensure it never panics on
// any input and always returns consistent error types.
func FuzzValidateGateEvidence(f *testing.F) {
	seeds := []struct {
		phase  string
		status string
		checks string // JSON array of check names, comma-separated
	}{
		{"1", "pass", "go-vet,go-test"},
		{"1", "fail", "go-vet"},
		{"2", "pass", "go-vet"},
		{"1", "unknown", ""},
		{"", "", ""},
		{"abc", "xyz", "check1,check2"},
	}
	for _, s := range seeds {
		f.Add(s.phase, s.status, s.checks)
	}

	f.Fuzz(func(t *testing.T, phase, status, checksCSV string) {
		ge := &GateEvidence{
			Phase:           phase,
			Status:          status,
			Timestamp:       time.Now().UTC(),
			DurationSeconds: 1.0,
		}

		// Parse comma-separated check names
		if checksCSV != "" {
			names := splitCSV(checksCSV)
			for _, name := range names {
				ge.Checks = append(ge.Checks, GateCheck{
					Name:   name,
					Status: "pass",
					Output: "fuzzed output",
				})
			}
		}

		// Must never panic
		err := ValidateGateEvidence(ge)
		if err != nil {
			// Error is expected for many inputs; just ensure it's non-nil
			// and meaningful
			if err.Error() == "" {
				t.Error("ValidateGateEvidence returned empty error message")
			}
		}
	})
}

// FuzzUnmarshalGateEvidence fuzzes the JSON unmarshalling gate evidence path
// with arbitrary bytes to ensure it never panics.
func FuzzUnmarshalGateEvidence(f *testing.F) {
	seeds := []string{
		`{"phase":"1","status":"pass","timestamp":"2026-07-29T12:00:00Z","duration_seconds":10,"checks":[]}`,
		`{"phase":"1"}`,
		`{}`,
		`null`,
		`not json at all`,
		`{"phase":"1","status":"pass","timestamp":"invalid","checks":[{"name":"test"}]}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		ge, err := UnmarshalGateEvidence(data)
		if err != nil {
			return // expected for invalid data
		}
		// If unmarshal succeeded, re-marshal must produce valid JSON
		out, err := MarshalGateEvidence(ge)
		if err != nil {
			t.Fatalf("re-marshal failed: %v", err)
		}
		var back GateEvidence
		if err := json.Unmarshal(out, &back); err != nil {
			t.Fatalf("re-unmarshal failed: %v", err)
		}
	})
}

// FuzzGateEvidencePassed fuzzes the GateEvidencePassed helper with various
// combinations of overall status and check statuses.
func FuzzGateEvidencePassed(f *testing.F) {
	seeds := []struct {
		status    string
		checkPass bool
	}{
		{"pass", true},
		{"pass", false},
		{"fail", true},
		{"fail", false},
	}
	for _, s := range seeds {
		f.Add(s.status, s.checkPass)
	}

	f.Fuzz(func(t *testing.T, status string, checkPass bool) {
		ge := &GateEvidence{
			Phase:     "1",
			Status:    status,
			Timestamp: time.Now().UTC(),
			Checks: []GateCheck{
				{Name: "test-check", Status: boolToStatus(checkPass), Output: "output"},
			},
		}
		result := GateEvidencePassed(ge)
		// Invariant: if status is not "pass" or any check fails, result is false
		if status != "pass" || !checkPass {
			if result {
				t.Errorf("GateEvidencePassed() = true, want false for status=%q checkPass=%v", status, checkPass)
			}
		}
	})
}

// splitCSV splits a comma-separated string, trimming whitespace.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			token := s[start:i]
			// trim
			startIdx := 0
			endIdx := len(token)
			for startIdx < endIdx && token[startIdx] == ' ' {
				startIdx++
			}
			for endIdx > startIdx && token[endIdx-1] == ' ' {
				endIdx--
			}
			if startIdx < endIdx {
				result = append(result, token[startIdx:endIdx])
			}
			start = i + 1
		}
	}
	return result
}

func boolToStatus(v bool) string {
	if v {
		return "pass"
	}
	return "fail"
}
