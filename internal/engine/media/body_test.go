package media

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestCopyLimitedWithinCeiling(t *testing.T) {
	src := bytes.NewReader(bytes.Repeat([]byte("a"), 100))
	var dst bytes.Buffer
	n, err := CopyLimited(context.Background(), &dst, src, 1000, "authority")
	if err != nil {
		t.Fatalf("CopyLimited err = %v", err)
	}
	if n != 100 || dst.Len() != 100 {
		t.Errorf("copied %d bytes, dst %d; want 100/100", n, dst.Len())
	}
}

func TestCopyLimitedExceedsCeiling(t *testing.T) {
	src := bytes.NewReader(bytes.Repeat([]byte("b"), 5000))
	var dst bytes.Buffer
	n, err := CopyLimited(context.Background(), &dst, src, 1000, "authority-x")
	if !IsPayloadTooLarge(err) {
		t.Fatalf("err = %v, want typed 413-class error", err)
	}
	if n > 1000 {
		t.Errorf("copied %d bytes before abort, want <= 1000", n)
	}
	if dst.Len() > 1000 {
		t.Errorf("dst grew to %d bytes, want <= 1000 (no full-buffer buffering)", dst.Len())
	}
}

func TestCopyLimitedCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := bytes.NewReader(bytes.Repeat([]byte("c"), 100))
	var dst bytes.Buffer
	_, err := CopyLimited(ctx, &dst, src, 1000, "a")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestCopyLimitedSlowSourceHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	slow := &slowReader{data: bytes.Repeat([]byte("d"), 4096), cancel: cancel, delay: 50 * time.Millisecond}
	var dst bytes.Buffer
	_, err := CopyLimited(ctx, &dst, slow, 1<<20, "a")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

type slowReader struct {
	data   []byte
	off    int
	cancel context.CancelFunc
	delay  time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.off >= len(s.data) {
		return 0, io.EOF
	}
	time.Sleep(s.delay)
	if s.off+len(p) > len(s.data) {
		p = p[:len(s.data)-s.off]
	}
	n := copy(p, s.data[s.off:])
	s.off += n
	s.cancel()
	return n, nil
}

func TestReadLimitedWithinCeiling(t *testing.T) {
	data, err := ReadLimited(strings.NewReader(`{"ok":true}`), 1024, "a")
	if err != nil {
		t.Fatalf("ReadLimited err = %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Errorf("data = %q", data)
	}
}

func TestReadLimitedExceedsCeiling(t *testing.T) {
	big := strings.Repeat("x", 5000)
	_, err := ReadLimited(strings.NewReader(big), 1000, "authority-y")
	if !IsPayloadTooLarge(err) {
		t.Fatalf("err = %v, want typed 413-class error", err)
	}
}

func TestReadLimitedExactlyAtCeiling(t *testing.T) {
	payload := strings.Repeat("y", 1000)
	data, err := ReadLimited(strings.NewReader(payload), 1000, "a")
	if err != nil {
		t.Fatalf("ReadLimited at ceiling err = %v", err)
	}
	if len(data) != 1000 {
		t.Errorf("len(data) = %d, want 1000", len(data))
	}
}
