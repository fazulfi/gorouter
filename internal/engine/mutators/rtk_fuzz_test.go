package mutators

import (
	"context"
	"testing"
)

func FuzzRTKCompress(f *testing.F) {
	seeds := []string{
		`{"messages":[]}`,
		`{"messages":[{"role":"user","content":"hi"}]}`,
		`{"messages":[{"role":"tool","tool_call_id":"c1","content":"short"}]}`,
		`{"messages":[{"role":"user","content":[{"type":"tool_result","content":"output"}]}]}`,
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		req := fixtureRequest(string(data))
		m := NewRTK(RTKConfig{Enabled: true})
		_, err := m.Mutate(context.Background(), req)
		if err != nil {
			t.Skip()
		}
	})
}

func FuzzRTKAutodetect(f *testing.F) {
	seeds := []string{
		"commit abc123\n",
		"diff --git a/f.go b/f.go\n",
		"On branch main\n",
		"foo/bar.go\n",
		"debug.log output\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data string) {
		fn := autoDetectFilter(data, 1024)
		if fn != nil && fn.fn != nil {
			_ = safeApplyFilter(fn, data)
		}
	})
}

func FuzzFilters(f *testing.F) {
	seeds := []string{
		"a\na\nb\nb\nc\nc\n",
		"line1\nline2\nline3\n",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data string) {
		fn := autoDetectFilter(data, 1024)
		if fn != nil && fn.fn != nil {
			safeApplyFilter(fn, data)
		}
	})
}
