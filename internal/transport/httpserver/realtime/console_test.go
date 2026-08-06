package realtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gorouter/internal/domain/console"
)

func consoleRow(seq int64, msg string) console.ConsoleLog {
	return console.ConsoleLog{Seq: seq, RedactedMessage: msg}
}

// TestConsoleStream asserts the console stream registry contract: init
// replays the buffered lines, line emits each new line, keepalive comments
// are emitted, and the handler aborts on disconnect without duplicate
// history.
func TestConsoleStream(t *testing.T) {
	src := newFakeConsoleSource()
	src.rows = []console.ConsoleLog{
		consoleRow(1, "started"),
		consoleRow(2, "listening on :8080"),
	}

	h := NewConsoleStream(src, 5*time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/console/stream", nil)

	frames, _ := framesOf(t, h, req, func() {
		src.mu.Lock()
		src.rows = append(src.rows, consoleRow(3, "new line"))
		src.mu.Unlock()
		src.notify()
	}, 40*time.Millisecond)

	events := eventSequence(t, frames)
	assertEventAt(t, events, 0, ConsoleInitEvent)
	assertEventAt(t, events, 1, ConsoleLineEvent)

	var init ConsoleInitPayload
	mustUnmarshal(t, []byte(frameDataFor(t, frames, ConsoleInitEvent, 0)), &init)
	if len(init.Lines) != 2 {
		t.Fatalf("init lines = %d, want 2", len(init.Lines))
	}
	if init.Lines[0].Seq != 1 || init.Lines[1].Seq != 2 {
		t.Errorf("init seqs = [%d %d], want [1 2]", init.Lines[0].Seq, init.Lines[1].Seq)
	}

	var line ConsoleLinePayload
	mustUnmarshal(t, []byte(frameDataFor(t, frames, ConsoleLineEvent, 0)), &line)
	if line.Seq != 3 || line.Message != "new line" {
		t.Errorf("line event = %+v, want seq 3 message %q", line, "new line")
	}

	if !hasKeepalive(frames) {
		t.Error("no keepalive comment frame emitted")
	}
	if !src.cancelled {
		t.Error("source Cancel was not called on disconnect")
	}
}

// TestConsoleStreamLastEventID asserts the server-cursor contract: a client
// that reconnects with Last-Event-ID replays only the lines strictly after
// that cursor, so no duplicate history is emitted.
func TestConsoleStreamLastEventID(t *testing.T) {
	src := newFakeConsoleSource()
	src.rows = []console.ConsoleLog{
		consoleRow(1, "one"),
		consoleRow(2, "two"),
		consoleRow(3, "three"),
	}

	h := NewConsoleStream(src, 5*time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/console/stream", nil)
	req.Header.Set("Last-Event-ID", "2")

	frames, _ := framesOf(t, h, req, nil, 15*time.Millisecond)

	var init ConsoleInitPayload
	mustUnmarshal(t, []byte(frameDataFor(t, frames, ConsoleInitEvent, 0)), &init)
	if len(init.Lines) != 1 {
		t.Fatalf("init lines after cursor 2 = %d, want 1 (no duplicate history)", len(init.Lines))
	}
	if init.Lines[0].Seq != 3 {
		t.Errorf("replayed seq = %d, want 3", init.Lines[0].Seq)
	}
}

// TestConsoleStreamServesRedactedOnly asserts the projection serves the
// redacted message field, never a raw channel.
func TestConsoleStreamServesRedactedOnly(t *testing.T) {
	src := newFakeConsoleSource()
	redacted := "[REDACTED]"
	level := "error"
	src.rows = []console.ConsoleLog{{Seq: 1, Level: &level, RedactedMessage: redacted}}

	h := NewConsoleStream(src, 0)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/console/stream", nil)

	frames, _ := framesOf(t, h, req, nil, 15*time.Millisecond)

	var init ConsoleInitPayload
	mustUnmarshal(t, []byte(frameDataFor(t, frames, ConsoleInitEvent, 0)), &init)
	if init.Lines[0].Message != "[REDACTED]" {
		t.Errorf("served message = %q, want the redacted form", init.Lines[0].Message)
	}
	if init.Lines[0].Level == nil || *init.Lines[0].Level != "error" {
		t.Errorf("level not preserved: %v", init.Lines[0].Level)
	}
}
