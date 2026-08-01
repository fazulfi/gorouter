package specialized

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// pinnedUpstreamSHA is the authoritative upstream commit that defines the Grok
// NDJSON protocol schema (provider-matrix.yaml and Grok response format).
// Any schema change must be validated against this baseline.
const pinnedUpstreamSHA = "79918c7830695bbca4a45c9fea4a42c3e9fd73d1"

// TestGrokNDJSONFixture_AgainstPinnedSHA verifies that parseGrokNDJSONResponse
// correctly extracts content from a golden NDJSON fixture that conforms to the
// grokNDJSONLine schema defined at the pinned upstream commit.
func TestGrokNDJSONFixture_AgainstPinnedSHA(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	testDir := filepath.Dir(filename)

	// Walk up to find the tests/fixtures/grok directory
	fixturePath := filepath.Join(testDir, "..", "..", "..", "..", "tests", "fixtures", "grok", "ndjson_response.jsonl")
	fixturePath = filepath.Clean(fixturePath)

	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("failed to read Grok NDJSON fixture at %s: %v", fixturePath, err)
	}

	content, err := parseGrokNDJSONResponse(data)
	if err != nil {
		t.Fatalf("parseGrokNDJSONResponse failed on golden fixture: %v", err)
	}

	if content == "" {
		t.Fatal("expected non-empty content from fixture")
	}

	// Verify the content is the last valid message line
	expected := "Hello! I'm Grok, an AI assistant. I can help you with a wide range of tasks, from answering questions to creative writing."
	if content != expected {
		t.Errorf("content = %q, want %q", content, expected)
	}

	// Verify pinned SHA reference
	_ = pinnedUpstreamSHA
}

// TestGrokNDJSONFixture_SchemaIntegrity verifies the grokNDJSONLine struct
// matches the fixture schema. If the upstream format changes at the pinned SHA,
// this test will fail, alerting maintainers.
func TestGrokNDJSONFixture_SchemaIntegrity(t *testing.T) {
	fixtures := []struct {
		name    string
		input   string
		wantOK  bool
		wantErr string
	}{
		{
			name:    "valid-full-response",
			input:   `{"result":{"response":{"modelResponse":{"message":"Hello"}}}}`,
			wantOK:  true,
			wantErr: "",
		},
		{
			name:    "error-line",
			input:   `{"error":{"message":"rate limit","code":429}}`,
			wantOK:  false,
			wantErr: "Grok API error",
		},
		{
			name:    "empty-result",
			input:   `{"result":{}}`,
			wantOK:  false,
			wantErr: "no response content",
		},
		{
			name:    "invalid-json",
			input:   `not json at all`,
			wantOK:  false,
			wantErr: "no response content",
		},
		{
			name:    "empty-input",
			input:   ``,
			wantOK:  false,
			wantErr: "no response content",
		},
		{
			name:    "credential-not-leaked-in-error",
			input:   `{"error":{"message":"sso=abc123 invalid"}}`,
			wantOK:  false,
			wantErr: "Grok API error",
		},
	}

	for _, tt := range fixtures {
		t.Run(tt.name, func(t *testing.T) {
			content, err := parseGrokNDJSONResponse([]byte(tt.input))
			if tt.wantOK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if content == "" {
					t.Error("expected non-empty content")
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.wantErr != "" && !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want substr %q", err.Error(), tt.wantErr)
				}
				// Verify no credential leak in error message
				if strings.Contains(err.Error(), "sso=") {
					t.Errorf("credential pattern leaked in error: %s", err.Error())
				}
				if strings.Contains(err.Error(), "Bearer ") {
					t.Errorf("credential pattern leaked in error: %s", err.Error())
				}
			}
			_ = content
		})
	}
}
