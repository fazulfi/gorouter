package performance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamingCancellation(t *testing.T) {
	cancelled := make(chan struct{})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			close(cancelled)
		case <-time.After(2 * time.Second):
			t.Error("handler did not observe cancellation")
		}
	})
	s := httptest.NewServer(h)
	defer s.Close()
	req, err := http.NewRequest(http.MethodGet, s.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	res, err := s.Client().Do(req)
	if err == nil {
		res.Body.Close()
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("cancellation was not observed")
	}
}
