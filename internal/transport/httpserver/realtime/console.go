package realtime

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"gorouter/internal/domain/console"
)

// ConsoleStreamSource is the read seam for the console stream: the durable
// console-log projection (internal/app/console serves ListAfter). Updates
// signals a new line batch; the handler emits one `line` event per new row
// strictly after the last delivered sequence number.
type ConsoleStreamSource interface {
	Updates() <-chan struct{}
	ListAfter(ctx context.Context, seq int64, limit int) ([]console.ConsoleLog, error)
}

// ConsoleLinePayload is one durable console line. Message always carries the
// redacted text served by the projection.
type ConsoleLinePayload struct {
	Seq        int64      `json:"seq"`
	Level      *string    `json:"level,omitempty"`
	Message    string     `json:"message"`
	OccurredAt *time.Time `json:"occurred_at,omitempty"`
}

// ConsoleInitPayload is the init replay: the buffered lines strictly after
// the client's cursor (or the most recent 50 when no cursor is presented).
type ConsoleInitPayload struct {
	Lines []ConsoleLinePayload `json:"lines"`
}

// NewConsoleStream returns the console SSE handler: an `init` replay of the
// buffered lines (strictly after the Last-Event-ID cursor when the client
// presents one, otherwise the most recent 50), then a `line` event per new
// line, keepalive comments at the interval bound, and abort when the request
// context is done. Last-Event-ID is used only as a server cursor into the
// durable console log, never as credentials. keepalive <= 0 selects the
// registry cadence (25 s).
func NewConsoleStream(src ConsoleStreamSource, keepalive time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := openStream(w)
		if err != nil {
			return
		}
		ctx := r.Context()
		cursor := lastEventIDCursor(r)
		if c, ok := src.(Canceler); ok {
			defer c.Cancel()
		}
		rows, err := src.ListAfter(ctx, cursor, console.DefaultCap)
		if err != nil {
			return
		}
		init := ConsoleInitPayload{Lines: make([]ConsoleLinePayload, 0, len(rows))}
		last := cursor
		for i := range rows {
			init.Lines = append(init.Lines, toConsoleLine(rows[i]))
			if rows[i].Seq > last {
				last = rows[i].Seq
			}
		}
		if err := writeEvent(w, f, ConsoleInitEvent, init); err != nil {
			return
		}
		ticker := time.NewTicker(interval(keepalive))
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				writeKeepalive(w, f)
			case <-src.Updates():
				rows, err := src.ListAfter(ctx, last, console.DefaultCap)
				if err != nil {
					return
				}
				for i := range rows {
					if err := writeEvent(w, f, ConsoleLineEvent, toConsoleLine(rows[i])); err != nil {
						return
					}
					if rows[i].Seq > last {
						last = rows[i].Seq
					}
				}
			}
		}
	}
}

// toConsoleLine projects one durable console row to the registry payload,
// serving the redacted message only.
func toConsoleLine(row console.ConsoleLog) ConsoleLinePayload {
	return ConsoleLinePayload{
		Seq:        row.Seq,
		Level:      row.Level,
		Message:    row.RedactedMessage,
		OccurredAt: row.OccurredAt,
	}
}

// lastEventIDCursor parses the Last-Event-ID header as a server cursor (a
// position in the durable console log). An absent or malformed value yields 0.
func lastEventIDCursor(r *http.Request) int64 {
	raw := r.Header.Get("Last-Event-ID")
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}
