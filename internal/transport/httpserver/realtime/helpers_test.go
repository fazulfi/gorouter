package realtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// sseFrame is one parsed SSE event block: an event/data pair or a comment
// line (keepalive).
type sseFrame struct {
	event   string
	data    string
	comment bool
}

// parseSSE splits an accumulated SSE body into its frames.
func parseSSE(t *testing.T, body string) []sseFrame {
	t.Helper()
	var frames []sseFrame
	for _, block := range strings.Split(body, "\n\n") {
		block = strings.TrimRight(block, "\n")
		if block == "" {
			continue
		}
		var f sseFrame
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event: "):
				f.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				f.data += strings.TrimPrefix(line, "data: ")
			case strings.HasPrefix(line, ":"):
				f.comment = true
			}
		}
		frames = append(frames, f)
	}
	return frames
}

// runStream serves h on a cancellable request context in a goroutine. The
// optional during callback runs after the initial push has landed so the
// test can mutate the fake source and trigger updates; the context is then
// cancelled and the test fails unless the handler returns, proving cleanup
// on r.Context().Done().
func runStream(t *testing.T, h http.HandlerFunc, req *http.Request, during func(), settle time.Duration) *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rr, req)
		close(done)
	}()
	time.Sleep(15 * time.Millisecond)
	if during != nil {
		during()
	}
	time.Sleep(settle)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not return after request context cancel (cleanup broken)")
	}
	return rr
}

// framesOf runs one stream request to completion and returns the parsed
// frames plus the response recorder.
func framesOf(t *testing.T, h http.HandlerFunc, req *http.Request, during func(), settle time.Duration) ([]sseFrame, *httptest.ResponseRecorder) {
	t.Helper()
	rr := runStream(t, h, req, during, settle)
	return parseSSE(t, rr.Body.String()), rr
}
