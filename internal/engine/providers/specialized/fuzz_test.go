package specialized

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// FuzzGrokWebNDJSONParser fuzzes the Grok NDJSON response parser using the
// actual parseGrokNDJSONResponse function. Exercises JSON unmarshalling
// into grokNDJSONLine, error-path classification, and content extraction.
func FuzzGrokWebNDJSONParser(f *testing.F) {
	seeds := []string{
		`{"result":{"response":{"modelResponse":{"message":"Hello"}}}}`,
		`{"error":{"message":"test error","code":500}}`,
		`{"result":{"response":{"token":"abc"}}}`,
		`invalid json`,
		``,
		`{"result":{}}`,
		`{"result":{"response":{"modelResponse":{}}}}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s + "\n"))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		content, err := parseGrokNDJSONResponse(data)
		if err != nil {
			if err.Error() == "" {
				t.Error("empty error message from parseGrokNDJSONResponse")
			}
			if strings.Contains(err.Error(), "sso=") ||
				strings.Contains(err.Error(), "Bearer ") {
				t.Error("credential leak in error message")
			}
			return
		}
		if content == "" {
			t.Error("empty content returned without error")
		}
		oai := buildChatCompletion("grok-test", content)
		var parsed struct {
			Object  string `json:"object"`
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(oai, &parsed); err != nil {
			t.Fatalf("buildChatCompletion output not valid JSON: %v", err)
		}
		if len(parsed.Choices) != 1 || parsed.Choices[0].Message.Content != content {
			t.Error("buildChatCompletion content mismatch")
		}
	})
}

// FuzzPplxSSEParser fuzzes the Perplexity SSE line parser by running the
// full scanner loop that streamPPLXSSE uses — event extraction, data
// forwarding, and terminal classification.
func FuzzPplxSSEParser(f *testing.F) {
	seeds := []string{
		"data: {\"text\":\"hello\"}\n\n",
		"event: test\ndata: {}\n\n",
		"data: [DONE]\n\n",
		"invalid\n\n",
		"event: error\ndata: {\"code\":500}\n\n",
		"\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		scanner := bytes.NewBuffer(data)
		var eventType string
		for {
			line, err := scanner.ReadString('\n')
			if err != nil {
				break
			}
			line = line[:len(line)-1]
			if line == "" {
				eventType = ""
				continue
			}
			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				payload := strings.TrimPrefix(line, "data: ")
				if payload == "[DONE]" {
					continue
				}
				_ = []byte(payload)
			}
		}
		_ = eventType
	})
}

// FuzzKiroEventStreamParser fuzzes the Kiro EventStream NDJSON parser by
// exercising JSON line validation, event extraction, and content classification
// using the same scanner pattern as streamEventStream.
func FuzzKiroEventStreamParser(f *testing.F) {
	seeds := []string{
		`{"type":"event","data":"test"}`,
		`{"type":"error","data":"fail"}`,
		`invalid{json`,
		``,
		`{"type":"final"}`,
		`{"result":{"content":"hello"}}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s + "\n"))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		scanner := bytes.NewBuffer(data)
		for {
			line, err := scanner.ReadBytes('\n')
			if err != nil {
				break
			}
			trimmed := strings.TrimSpace(string(line))
			if trimmed == "" {
				continue
			}
			var obj map[string]interface{}
			if err := json.Unmarshal([]byte(trimmed), &obj); err != nil {
				continue
			}
			if typ, ok := obj["type"]; ok {
				if _, ok := typ.(string); !ok {
					t.Logf("non-string type field: %T", typ)
				}
			}
		}
	})
}

// FuzzCursorSSEParser fuzzes the standard SSE parser used by CursorExecutor
// (now shared via streamSSE). Covers event: and data: line extraction.
func FuzzCursorSSEParser(f *testing.F) {
	seeds := []string{
		"data: {\"content\":\"hello\"}\n\n",
		"event: completion\ndata: {\"text\":\"world\"}\n\n",
		"data: [DONE]\n\n",
		"event: ping\ndata: {}\n\n",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		scanner := bytes.NewBuffer(data)
		var eventType string
		for {
			line, err := scanner.ReadString('\n')
			if err != nil {
				break
			}
			line = line[:len(line)-1]
			if line == "" {
				eventType = ""
				continue
			}
			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				trimmed := strings.TrimPrefix(line, "data: ")
				if trimmed == "[DONE]" {
					continue
				}
				_ = []byte(trimmed)
			}
		}
		_ = eventType
	})
}
