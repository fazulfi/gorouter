package realtime

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenStreamSetsWireHeaders(t *testing.T) {
	w := httptest.NewRecorder()
	f, err := openStream(w)
	if err != nil {
		t.Fatalf("openStream: %v", err)
	}
	if f == nil {
		t.Fatal("openStream returned nil flusher")
	}
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if got := w.Header().Get("Connection"); got != "keep-alive" {
		t.Errorf("Connection = %q, want keep-alive", got)
	}
}

func TestWriteKeepaliveCommentFormat(t *testing.T) {
	w := httptest.NewRecorder()
	writeKeepalive(w, w)
	if got := w.Body.String(); got != ": keepalive\n\n" {
		t.Errorf("keepalive frame = %q, want %q", got, ": keepalive\n\n")
	}
}

func TestWriteEventAppliesOutboundRedaction(t *testing.T) {
	w := httptest.NewRecorder()
	payload := struct {
		Line string `json:"line"`
	}{
		Line: "provider auth api_key_value=sk-proj-abc123xyz failed",
	}
	if err := writeEvent(w, w, UsageStatsEvent, payload); err != nil {
		t.Fatalf("writeEvent: %v", err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: "+UsageStatsEvent) {
		t.Errorf("event line missing: %q", body)
	}
	if !strings.Contains(body, "[REDACTED]") {
		t.Errorf("redaction marker missing in %q", body)
	}
	if strings.Contains(body, "sk-proj-abc123xyz") {
		t.Errorf("credential-shaped value survived the outbound redaction pass: %q", body)
	}
}

func TestWriteEventCleanPayloadUnchanged(t *testing.T) {
	w := httptest.NewRecorder()
	payload := StatsPayload{TotalRequests: 7, Period: "24h"}
	if err := writeEvent(w, w, UsageStatsEvent, payload); err != nil {
		t.Fatalf("writeEvent: %v", err)
	}
	var data string
	for _, line := range strings.Split(w.Body.String(), "\n") {
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	var back StatsPayload
	if err := json.Unmarshal([]byte(data), &back); err != nil {
		t.Fatalf("data line is not clean JSON: %v", err)
	}
	if back.TotalRequests != 7 {
		t.Errorf("round-trip TotalRequests = %d, want 7", back.TotalRequests)
	}
}

func TestLastEventIDCursor(t *testing.T) {
	cases := []struct {
		header string
		want   int64
	}{
		{"", 0},
		{"0", 0},
		{"42", 42},
		{"abc", 0},
		{"-3", 0},
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, "/console/stream", nil)
		if c.header != "" {
			req.Header.Set("Last-Event-ID", c.header)
		}
		if got := lastEventIDCursor(req); got != c.want {
			t.Errorf("Last-Event-ID %q = %d, want %d", c.header, got, c.want)
		}
	}
}

func TestIntervalDefaultsToRegistryCadence(t *testing.T) {
	if got := interval(0); got != KeepaliveInterval {
		t.Errorf("interval(0) = %v, want %v", got, KeepaliveInterval)
	}
	if got := interval(5 * KeepaliveInterval); got != 5*KeepaliveInterval {
		t.Errorf("interval(5x) = %v, want 5x cadence", got)
	}
}
