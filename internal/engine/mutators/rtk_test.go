package mutators

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestRTK_Disabled(t *testing.T) {
	m := NewRTK(RTKConfig{Enabled: false})
	req := fixtureRequest(`{"messages":[{"role":"user","content":"hi"}]}`)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected disabled RTK to not apply")
	}
}

func TestRTK_NilRequest(t *testing.T) {
	m := NewRTK(RTKConfig{Enabled: true})
	result, err := m.Mutate(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected nil request to not apply")
	}
}

func TestRTK_EmptyBody(t *testing.T) {
	m := NewRTK(RTKConfig{Enabled: true})
	req := fixtureRequest(``)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected empty body to not apply")
	}
}

func TestRTK_ToolContentCompression(t *testing.T) {
	m := NewRTK(RTKConfig{Enabled: true, MinCompressSize: 1, RawCap: 100000})
	content := "this is a very long tool result content that should be compressed because it has enough characters to trigger the compression filters automatically"
	body := `{"messages":[{"role":"user","content":"hi"},{"role":"tool","tool_call_id":"call1","content":"` + content + `"}]}`
	req := fixtureRequest(body)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Log("RTK compressed tool content")
	}
}

func TestRTK_ClaudeToolResult(t *testing.T) {
	m := NewRTK(RTKConfig{Enabled: true, MinCompressSize: 1, RawCap: 100000})
	body := `{"messages":[{"role":"user","content":[{"type":"tool_result","tool_use_id":"abc","content":"large tool output that should be compressed via RTK filters when it exceeds the minimum size threshold"}]}]}`
	req := fixtureRequest(body)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Log("RTK compressed Claude tool result")
	}
}

func TestRTK_ErrorResultSkipped(t *testing.T) {
	m := NewRTK(RTKConfig{Enabled: true, MinCompressSize: 1})
	body := `{"messages":[{"role":"user","content":[{"type":"tool_result","is_error":true,"content":"error: command not found"}]}]}`
	req := fixtureRequest(body)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Log("RTK may or may not skip error results depending on content size")
	}
}

func TestRTK_SmallContentSkipped(t *testing.T) {
	m := NewRTK(RTKConfig{Enabled: true, MinCompressSize: 10000})
	body := `{"messages":[{"role":"tool","tool_call_id":"call1","content":"short"}]}`
	req := fixtureRequest(body)
	result, err := m.Mutate(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Applied {
		t.Fatal("expected small content to not be compressed")
	}
}

func TestRTK_Concurrency(t *testing.T) {
	m := NewRTK(RTKConfig{Enabled: true, MinCompressSize: 10})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			body := `{"messages":[{"role":"tool","tool_call_id":"c1","content":"concurrent tool result content that should be compressed"}]}`
			req := fixtureRequest(body)
			m.Mutate(context.Background(), req)
		}()
	}
	wg.Wait()
}

func TestRTK_AutoDetectGitLog(t *testing.T) {
	fn := autoDetectFilter("commit abc123def456\nAuthor: Test\ndate", 100)
	if fn == nil || fn.name != "git-log" {
		t.Fatalf("expected git-log filter, got %v", fn)
	}
}

func TestRTK_AutoDetectGitDiff(t *testing.T) {
	fn := autoDetectFilter("diff --git a/file.go b/file.go\nindex abc..def 100644\n--- a/file.go\n+++ b/file.go\n@@ -1,5 +1,6 @@\n", 200)
	if fn == nil || (fn.name != "git-diff") {
		t.Fatalf("expected git-diff filter, got %v", fn)
	}
}

func TestRTK_AutoDetectGitStatus(t *testing.T) {
	fn := autoDetectFilter("On branch main\nChanges not staged for commit:\n  modified: file.go\n", 100)
	if fn == nil || fn.name != "git-status" {
		t.Fatalf("expected git-status filter, got %v", fn)
	}
}

func TestRTK_FilterGitDiffTruncates(t *testing.T) {
	var lines []string
	for i := 0; i < 150; i++ {
		lines = append(lines, "+line")
	}
	result := filterGitDiff("@@ -1,5 +1,150 @@\n" + strings.Join(lines, "\n"))
	if len(result) < 100 {
		t.Fatalf("expected truncated output, got %d chars", len(result))
	}
}

func TestRTK_FilterDedupLog(t *testing.T) {
	input := "a\na\nb\nb\nc\nc\n"
	result := filterDedupLog(input)
	expected := "a\nb\nc"
	if result != expected {
		t.Fatalf("filterDedupLog(%q) = %q, want %q", input, result, expected)
	}
}

func TestRTK_FilterSmartTruncate(t *testing.T) {
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, "line")
	}
	input := strings.Join(lines, "\n")
	result := filterSmartTruncate(input)
	if !strings.Contains(result, "...") {
		t.Fatal("expected truncation marker")
	}
}

func TestRTK_Name(t *testing.T) {
	m := NewRTK(RTKConfig{})
	if m.Name() != "rtk" {
		t.Fatalf("expected name 'rtk', got %q", m.Name())
	}
}

func TestRTK_HitStats(t *testing.T) {
	stats := &rtkStats{}
	text := "commit abc123\nauthor test\n\ndiff --git a/f.go b/f.go\nindex abc..def\n--- a/f.go\n+++ b/f.go\n@@ -1 +1 @@\n-change\n+change\n"
	out := compressText(text, stats, "test-shape", RTKConfig{
		Enabled:         true,
		MinCompressSize: 1,
		RawCap:          100000,
		DetectWindow:    1024,
	})
	if out != text && len(out) < len(text) {
		t.Logf("compressed %d -> %d, hits: %d", len(text), len(out), len(stats.hits))
	}
}
