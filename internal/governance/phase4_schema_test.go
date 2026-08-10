//go:build !no_schema_conformance_test

package governance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestAllGateEvidenceFilesConformToSchemaV2 validates all shipped gate evidence
// documents (phases 1-4) against the strict contract defined by schema.json v2.
// This makes schema violations visible during go test ./internal/governance/...
// instead of slipping through Go's silent JSON field discarding.
func TestAllGateEvidenceFilesConformToSchemaV2(t *testing.T) {
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}

	schemaPath := filepath.Join(root, "docs", "implementation", "gate-evidence", "schema.json")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read schema.json: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parse schema.json: %v", err)
	}

	files := []string{"phase-1.json", "phase-2.json", "phase-3.json", "phase-4.json"}
	for _, fn := range files {
		path := filepath.Join(root, "docs", "implementation", "gate-evidence", fn)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v", fn, err)
		}
		if err := validateAgainstSchemaV2(raw, schema); err != nil {
			t.Errorf("%s violates schema v2: %v", fn, err)
		}
	}
}

func loadSchemaV2ForTest(t *testing.T) map[string]any {
	t.Helper()
	root, err := RootDir()
	if err != nil {
		t.Skip("project root not found:", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "docs", "implementation", "gate-evidence", "schema.json"))
	if err != nil {
		t.Fatalf("read schema.json: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parse schema.json: %v", err)
	}
	return schema
}

// TestSchemaV2RejectsDuplicateChecks is the regression fixture for review
// finding I-1: schema.json v2 declares checks.uniqueItems true (line 65), so a
// gate evidence document containing two identical check items must fail
// validation. Before the I-1 fix validateAgainstSchemaV2 silently accepted
// duplicates, which made this test fail (observed RED).
func TestSchemaV2RejectsDuplicateChecks(t *testing.T) {
	schema := loadSchemaV2ForTest(t)
	const duplicateChecksPayload = `{
  "phase": "4",
  "status": "pending",
  "timestamp": "2026-08-07T02:00:00Z",
  "duration_seconds": 0,
  "checks": [
    {"name": "example-check", "status": "pending", "output": "first occurrence"},
    {"name": "example-check", "status": "pending", "output": "first occurrence"}
  ]
}`
	err := validateAgainstSchemaV2([]byte(duplicateChecksPayload), schema)
	if err == nil {
		t.Fatal("validator accepted duplicate check items; schema.json declares checks.uniqueItems true")
	}
	if !strings.Contains(err.Error(), "uniqueItems") {
		t.Errorf("validation error should cite uniqueItems, got: %v", err)
	}
}

// TestSchemaV2UniqueItemsComparesWholeItems pins the JSON Schema semantics of
// uniqueItems: whole-item deep equality, not name equality. Two checks that
// share a name but differ in output are NOT duplicates under schema v2, so the
// validator must accept them (it enforces the authoritative schema, nothing
// stricter).
func TestSchemaV2UniqueItemsComparesWholeItems(t *testing.T) {
	schema := loadSchemaV2ForTest(t)
	const sameNameDifferentOutputPayload = `{
  "phase": "4",
  "status": "pending",
  "timestamp": "2026-08-07T02:00:00Z",
  "duration_seconds": 0,
  "checks": [
    {"name": "example-check", "status": "pending", "output": "first occurrence"},
    {"name": "example-check", "status": "pending", "output": "second occurrence"}
  ]
}`
	if err := validateAgainstSchemaV2([]byte(sameNameDifferentOutputPayload), schema); err != nil {
		t.Fatalf("validator rejected schema-valid distinct items with a shared name: %v", err)
	}
}

// validateAgainstSchemaV2 strictly validates raw JSON bytes against the extracted
// contract from schema.json v2. Returns aggregated errors for any violation.
func validateAgainstSchemaV2(raw []byte, schema map[string]any) error {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("invalid JSON object: %w", err)
	}

	var errs []string

	props, _ := schema["properties"].(map[string]any)

	// Required top-level fields
	requiredRaw, _ := schema["required"].([]any)
	var required []string
	for _, r := range requiredRaw {
		if s, ok := r.(string); ok {
			required = append(required, s)
		}
	}
	for _, f := range required {
		if _, has := doc[f]; !has {
			errs = append(errs, fmt.Sprintf(`missing required top-level field %q`, f))
		}
	}

	// Allowed top-level keys (from properties)
	topAllowed := make(map[string]bool)
	for k := range props {
		topAllowed[k] = true
	}
	// Schema v2 has additionalProperties: false at top level
	if topClosed, _ := schema["additionalProperties"].(bool); topClosed == false {
		extra := []string{}
		for k := range doc {
			if !topAllowed[k] {
				extra = append(extra, k)
			}
		}
		sort.Strings(extra)
		for _, k := range extra {
			errs = append(errs, fmt.Sprintf(`forbidden top-level field %q (additionalProperties is false)`, k))
		}
	}

	// Validate individual top-level fields
	if p, ok := doc["phase"].(string); ok {
		matched, _ := regexp.MatchString("^[0-9]+$", p)
		if !matched {
			errs = append(errs, fmt.Sprintf(`phase %q does not match pattern ^[0-9]+$`, p))
		}
	} else {
		errs = append(errs, "phase must be a string")
	}

	statusStr, _ := doc["status"].(string)
	enumList, _ := props["status"].(map[string]any)["enum"].([]any)
	statusOk := false
	validStatus := []string{}
	for _, e := range enumList {
		if s, ok := e.(string); ok {
			validStatus = append(validStatus, s)
			if statusStr == s {
				statusOk = true
			}
		}
	}
	if !statusOk {
		sort.Strings(validStatus)
		errs = append(errs, fmt.Sprintf(`status %q not in enum %v`, statusStr, validStatus))
	}

	if ts, ok := doc["timestamp"].(string); ok {
		_, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			errs = append(errs, fmt.Sprintf(`timestamp %q not RFC3339: %v`, ts, err))
		}
	} else {
		errs = append(errs, "timestamp must be a string")
	}

	durVal, _ := doc["duration_seconds"].(float64)
	minDur, _ := props["duration_seconds"].(map[string]any)["minimum"].(float64)
	if durVal < minDur {
		errs = append(errs, fmt.Sprintf("duration_seconds=%.2f below minimum %.2f", durVal, minDur))
	} else if !(durVal >= 0) {
		errs = append(errs, "duration_seconds must be >= 0")
	}

	checksRaw, _ := doc["checks"].([]any)
	minChecks, _ := props["checks"].(map[string]any)["minItems"].(float64)
	if len(checksRaw) < int(minChecks) {
		errs = append(errs, fmt.Sprintf("checks has %d items, minimum %d", len(checksRaw), int(minChecks)))
	}
	checksUnique, _ := props["checks"].(map[string]any)["uniqueItems"].(bool)

	checkRequiredRaw, _ := props["checks"].(map[string]any)["items"].(map[string]any)["required"].([]any)
	var checkRequired []string
	for _, r := range checkRequiredRaw {
		if s, ok := r.(string); ok {
			checkRequired = append(checkRequired, s)
		}
	}

	checkItems, _ := props["checks"].(map[string]any)["items"].(map[string]any)
	checkEnumList, _ := checkItems["properties"].(map[string]any)["status"].(map[string]any)["enum"].([]any)
	var checkValidStatus []string
	for _, e := range checkEnumList {
		if s, ok := e.(string); ok {
			checkValidStatus = append(checkValidStatus, s)
		}
	}
	sort.Strings(checkValidStatus)

	checkAllowed := make(map[string]bool)
	for k := range checkItems["properties"].(map[string]any) {
		checkAllowed[k] = true
	}
	checkClosed, _ := checkItems["additionalProperties"].(bool)

	for i, cr := range checksRaw {
		if checksUnique {
			for j := 0; j < i; j++ {
				if reflect.DeepEqual(cr, checksRaw[j]) {
					errs = append(errs, fmt.Sprintf("checks[%d] is identical to checks[%d] (uniqueItems is true)", i, j))
					break
				}
			}
		}

		check, ok := cr.(map[string]any)
		if !ok {
			errs = append(errs, fmt.Sprintf("checks[%d] must be an object", i))
			continue
		}

		for _, rf := range checkRequired {
			if _, has := check[rf]; !has {
				errs = append(errs, fmt.Sprintf("checks[%d] missing required field %q", i, rf))
			}
		}

		if checkClosed == false {
			extra := []string{}
			for kf := range check {
				if !checkAllowed[kf] {
					extra = append(extra, kf)
				}
			}
			sort.Strings(extra)
			for _, kf := range extra {
				errs = append(errs, fmt.Sprintf("checks[%d] forbidden field %q", i, kf))
			}
		}

		csStr, _ := check["status"].(string)
		csOk := false
		for _, ec := range checkValidStatus {
			if csStr == ec {
				csOk = true
				break
			}
		}
		if !csOk {
			errs = append(errs, fmt.Sprintf("checks[%d].status %q invalid", i, csStr))
		}
	}

	if len(errs) > 0 {
		sort.Strings(errs)
		return fmt.Errorf("validation failed: %s", strings.Join(errs, "; "))
	}
	return nil
}
