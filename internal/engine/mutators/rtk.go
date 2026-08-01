package mutators

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strings"

	"gorouter/internal/domain/engine"
)

const (
	rawCap          = 10 * 1024 * 1024
	minCompressSize = 500
	detectWindow    = 1024
)

type RTKConfig struct {
	Enabled         bool
	RawCap          int
	MinCompressSize int
	DetectWindow    int
}

func DefaultRTKConfig() RTKConfig {
	return RTKConfig{
		Enabled:         false,
		RawCap:          rawCap,
		MinCompressSize: minCompressSize,
		DetectWindow:    detectWindow,
	}
}

type rtkMutator struct {
	cfg RTKConfig
}

func NewRTK(cfg RTKConfig) Mutator {
	return &rtkMutator{cfg: cfg}
}

func (m *rtkMutator) Name() string { return "rtk" }

func (m *rtkMutator) Mutate(_ context.Context, req *engine.Request) (*Result, error) {
	if !m.cfg.Enabled {
		return &Result{Applied: false}, nil
	}
	if req == nil || len(req.MappedBody) == 0 {
		return &Result{Applied: false}, nil
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(req.MappedBody, &body); err != nil {
		return &Result{Applied: false}, nil
	}

	stats := &rtkStats{}
	msgs := extractMessages(body)
	if len(msgs) == 0 {
		return &Result{Applied: false}, nil
	}
	if !compressMessages(msgs, stats, m.cfg) {
		return &Result{Applied: false}, nil
	}
	raw, err := marshalMessages(body, msgs)
	if err != nil {
		return &Result{Applied: false}, nil
	}
	req.MappedBody = raw
	return &Result{
		Applied: true,
		Stats: map[string]interface{}{
			"bytes_before": stats.bytesBefore,
			"bytes_after":  stats.bytesAfter,
			"hits":         stats.hits,
		},
	}, nil
}

type rtkStats struct {
	bytesBefore int
	bytesAfter  int
	hits        []rtkHit
}

type rtkHit struct {
	Shape  string `json:"shape"`
	Filter string `json:"filter"`
	Saved  int    `json:"saved"`
}

func extractMessages(body map[string]json.RawMessage) []json.RawMessage {
	if msgs, ok := body["messages"]; ok {
		var arr []json.RawMessage
		if err := json.Unmarshal(msgs, &arr); err == nil {
			return arr
		}
	}
	if msgs, ok := body["input"]; ok {
		var arr []json.RawMessage
		if err := json.Unmarshal(msgs, &arr); err == nil {
			return arr
		}
	}
	return nil
}

func marshalMessages(body map[string]json.RawMessage, msgs []json.RawMessage) ([]byte, error) {
	raw, err := json.Marshal(msgs)
	if err != nil {
		return nil, err
	}
	if _, ok := body["messages"]; ok {
		body["messages"] = raw
	} else if _, ok := body["input"]; ok {
		body["input"] = raw
	}
	return json.Marshal(body)
}

func compressMessages(msgs []json.RawMessage, stats *rtkStats, cfg RTKConfig) bool {
	changed := false
	for i := range msgs {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(msgs[i], &msg); err != nil {
			continue
		}
		if roleBytes, ok := msg["role"]; ok {
			var role string
			if err := json.Unmarshal(roleBytes, &role); err == nil && role == "tool" {
				if content, ok := msg["content"]; ok {
					var strContent string
					if err := json.Unmarshal(content, &strContent); err == nil {
						before := len(strContent)
						if compressed := compressText(strContent, stats, "openai-tool", cfg); compressed != strContent {
							raw, _ := json.Marshal(compressed)
							msg["content"] = raw
							msgs[i], _ = json.Marshal(msg)
							changed = true
						}
						if changed {
							continue
						}
						stats.bytesBefore += before
						stats.bytesAfter += before
						continue
					}
					var arr []json.RawMessage
					if err := json.Unmarshal(content, &arr); err == nil {
						totalBefore := 0
						for _, a := range arr {
							totalBefore += len(a)
						}
						stats.bytesBefore += totalBefore
						if compressContentArray(arr, stats, "openai-tool-array", cfg) {
							raw, _ := json.Marshal(arr)
							msg["content"] = raw
							msgs[i], _ = json.Marshal(msg)
							changed = true
						} else {
							stats.bytesAfter += totalBefore
						}
					}
				}
				continue
			}
		}
		if content, ok := msg["content"]; ok {
			var blocks []json.RawMessage
			if err := json.Unmarshal(content, &blocks); err != nil {
				continue
			}
			if compressContentBlocks(blocks, stats, cfg) {
				raw, _ := json.Marshal(blocks)
				msg["content"] = raw
				msgs[i], _ = json.Marshal(msg)
				changed = true
			}
		}
	}
	return changed
}

func compressContentBlocks(blocks []json.RawMessage, stats *rtkStats, cfg RTKConfig) bool {
	changed := false
	for j := range blocks {
		var block map[string]json.RawMessage
		if err := json.Unmarshal(blocks[j], &block); err != nil {
			continue
		}
		typeBytes, ok := block["type"]
		if !ok {
			continue
		}
		var blockType string
		if err := json.Unmarshal(typeBytes, &blockType); err != nil || blockType != "tool_result" {
			continue
		}
		if isError, ok := block["is_error"]; ok {
			var errVal bool
			if json.Unmarshal(isError, &errVal) == nil && errVal {
				continue
			}
		}
		if content, ok := block["content"]; ok {
			var strContent string
			if err := json.Unmarshal(content, &strContent); err == nil {
				before := len(strContent)
				stats.bytesBefore += before
				if compressed := compressText(strContent, stats, "claude-string", cfg); compressed != strContent {
					raw, _ := json.Marshal(compressed)
					block["content"] = raw
					blocks[j], _ = json.Marshal(block)
					changed = true
				} else {
					stats.bytesAfter += before
				}
				continue
			}
			var arr []json.RawMessage
			if err := json.Unmarshal(content, &arr); err == nil {
				totalBefore := 0
				for _, a := range arr {
					totalBefore += len(a)
				}
				stats.bytesBefore += totalBefore
				if compressContentArray(arr, stats, "claude-array", cfg) {
					raw, _ := json.Marshal(arr)
					block["content"] = raw
					blocks[j], _ = json.Marshal(block)
					changed = true
				} else {
					stats.bytesAfter += totalBefore
				}
			}
		}
	}
	return changed
}

func compressContentArray(parts []json.RawMessage, stats *rtkStats, shape string, cfg RTKConfig) bool {
	changed := false
	for k := range parts {
		var part map[string]json.RawMessage
		if err := json.Unmarshal(parts[k], &part); err != nil {
			continue
		}
		typeBytes, ok := part["type"]
		if !ok {
			continue
		}
		var partType string
		if err := json.Unmarshal(typeBytes, &partType); err != nil || partType != "text" {
			continue
		}
		if textBytes, ok := part["text"]; ok {
			var text string
			if err := json.Unmarshal(textBytes, &text); err == nil {
				before := len(text)
				stats.bytesBefore += before
				if compressed := compressText(text, stats, shape, cfg); compressed != text {
					raw, _ := json.Marshal(compressed)
					part["text"] = raw
					parts[k], _ = json.Marshal(part)
					changed = true
				} else {
					stats.bytesAfter += before
				}
			}
		}
	}
	return changed
}

func compressText(text string, stats *rtkStats, shape string, cfg RTKConfig) string {
	bytesIn := len(text)
	window := cfg.DetectWindow
	if window <= 0 {
		window = 1024
	}
	if bytesIn < cfg.MinCompressSize || bytesIn > cfg.RawCap {
		return text
	}
	fn := autoDetectFilter(text, window)
	if fn == nil {
		return text
	}
	out := safeApplyFilter(fn, text)
	if out == "" || len(out) >= bytesIn {
		return text
	}
	stats.hits = append(stats.hits, rtkHit{
		Shape:  shape,
		Filter: fn.name,
		Saved:  bytesIn - len(out),
	})
	return out
}

var (
	reGitDiff     = regexp.MustCompile(`(?m)^diff --git `)
	reGitDiffHunk = regexp.MustCompile(`(?m)^@@ `)
	reGitStatus   = regexp.MustCompile(`(?m)^On branch |^nothing to commit|^Changes (not |to be )|^Untracked files:`)
	reGitLog      = regexp.MustCompile(`(?m)^[*|/\ ]*commit [0-9a-f]{7,40}$`)
	rePorcelain   = regexp.MustCompile(`^[ MADRCU?!][ MADRCU?!] \S`)
	reBuildOutput = regexp.MustCompile(`(?im)^(npm (warn|error|ERR!)|yarn (warn|error)|\s*Compiling\s+\S+|\s*Downloading\s+\S+|added \d+ package|\[ERROR\]|BUILD (SUCCESS|FAILED)|\s*Finished\s+|Successfully (installed|built)|ERROR:)`)
	reTreeGlyph   = regexp.MustCompile(`[├└]──|│  `)
	reLSRow       = regexp.MustCompile(`(?m)^[-dlbcps][rwx-]{9}`)
	reLSTotal     = regexp.MustCompile(`(?m)^total \d+$`)
	reGrepLine    = regexp.MustCompile(`^[^\s:]+:\d+:`)
)

type detectedFilter struct {
	name string
	fn   func(string) string
}

func autoDetectFilter(text string, window int) *detectedFilter {
	if window <= 0 || window > len(text) {
		window = len(text)
	}
	head := text[:window]

	if reGitLog.MatchString(head) {
		return &detectedFilter{name: "git-log", fn: filterGitLog}
	}
	if reGitDiff.MatchString(head) || reGitDiffHunk.MatchString(head) {
		return &detectedFilter{name: "git-diff", fn: filterGitDiff}
	}
	if reGitStatus.MatchString(head) {
		return &detectedFilter{name: "git-status", fn: filterGitStatus}
	}
	if reBuildOutput.MatchString(head) {
		return &detectedFilter{name: "build-output", fn: filterBuildOutput}
	}
	if isMostlyPorcelain(head) {
		return &detectedFilter{name: "git-status", fn: filterGitStatus}
	}

	lines := strings.Split(head, "\n")
	nonEmpty := filterEmpty(lines)
	sample := nonEmpty
	if len(sample) > 5 {
		sample = sample[:5]
	}
	for _, line := range sample {
		if isGrepLine(line) {
			return &detectedFilter{name: "grep", fn: filterGrep}
		}
	}
	if len(nonEmpty) >= 3 && allPathLike(nonEmpty) {
		return &detectedFilter{name: "find", fn: filterFind}
	}
	if reTreeGlyph.MatchString(head) {
		return &detectedFilter{name: "tree", fn: filterTree}
	}
	if reLSTotal.MatchString(head) || countMatches(head, reLSRow) >= 3 {
		return &detectedFilter{name: "ls", fn: filterLS}
	}
	if len(lines) >= 250 && isLineNumbered(lines) {
		return &detectedFilter{name: "read-numbered", fn: filterReadNumbered}
	}
	if len(nonEmpty) >= 5 {
		return &detectedFilter{name: "dedup-log", fn: filterDedupLog}
	}
	if len(lines) >= 250 {
		return &detectedFilter{name: "smart-truncate", fn: filterSmartTruncate}
	}
	return nil
}

const (
	hunkMaxLines  = 100
	logMaxLines   = 200
	dedupCap      = 2000
	grepCap       = 50
	findCap       = 20
	statusCapFile = 10
	statusCapUntr = 10
	smartHead     = 120
	smartTail     = 60
	smartMin      = 250
	numMinHit     = 0.7
	treeCap       = 200
	lsCap         = 50
	rnCap         = 200
	buildCap      = 50
)

func filterGitDiff(text string) string {
	var out bytes.Buffer
	lines := strings.Split(text, "\n")
	hunk := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "@@ ") {
			hunk = 0
			out.WriteString(line + "\n")
			continue
		}
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") || strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "new file ") || strings.HasPrefix(line, "deleted file ") {
			hunk = 0
			out.WriteString(line + "\n")
			continue
		}
		if hunk >= 0 {
			if hunk < hunkMaxLines {
				out.WriteString(line + "\n")
				hunk++
			} else {
				out.WriteString(fmt.Sprintf("@@ ... +%d ... @@ [%d lines suppressed]\n", hunk, hunkMaxLines))
				hunk = -1
			}
		}
	}
	return strings.TrimRight(out.String(), "\n")
}

func filterGitStatus(text string) string {
	var out bytes.Buffer
	lines := strings.Split(text, "\n")
	fc := 0
	uc := 0
	untr := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "On branch ") || strings.HasPrefix(line, "nothing to commit") || strings.HasPrefix(line, "Changes ") || strings.HasPrefix(line, "Untracked files:") {
			out.WriteString(line + "\n")
			untr = strings.HasPrefix(line, "Untracked files:")
			fc = 0
			continue
		}
		if untr {
			if uc < statusCapUntr {
				out.WriteString(line + "\n")
				uc++
			}
			continue
		}
		if rePorcelain.MatchString(line) || strings.HasPrefix(line, "	") {
			if fc < statusCapFile {
				out.WriteString(line + "\n")
				fc++
			}
			continue
		}
		out.WriteString(line + "\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func filterGitLog(text string) string {
	var out bytes.Buffer
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i >= logMaxLines {
			out.WriteString(fmt.Sprintf("  ... (%d more commits suppressed)\n", len(lines)-i))
			break
		}
		out.WriteString(line + "\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func filterBuildOutput(text string) string {
	var out bytes.Buffer
	lines := strings.Split(text, "\n")
	c := 0
	for _, line := range lines {
		if c >= buildCap {
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		out.WriteString(line + "\n")
		c++
	}
	return strings.TrimRight(out.String(), "\n")
}

func filterGrep(text string) string {
	var out bytes.Buffer
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i >= grepCap {
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		out.WriteString(line + "\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func filterFind(text string) string {
	var out bytes.Buffer
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i >= findCap {
			out.WriteString(fmt.Sprintf("[%d more entries suppressed]\n", len(lines)-i))
			break
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		out.WriteString(line + "\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func filterTree(text string) string {
	var out bytes.Buffer
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i >= treeCap {
			out.WriteString(fmt.Sprintf("[%d more lines suppressed]\n", len(lines)-i))
			break
		}
		out.WriteString(line + "\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func filterLS(text string) string {
	var out bytes.Buffer
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i >= lsCap {
			break
		}
		out.WriteString(line + "\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func filterReadNumbered(text string) string {
	var out bytes.Buffer
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if i >= rnCap {
			out.WriteString(fmt.Sprintf("... (%d more lines)\n", len(lines)-i))
			break
		}
		out.WriteString(line + "\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func filterSmartTruncate(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) < smartMin {
		return text
	}
	var out bytes.Buffer
	for i := 0; i < smartHead && i < len(lines); i++ {
		out.WriteString(lines[i] + "\n")
	}
	mid := len(lines) - smartHead - smartTail
	if mid < 0 {
		mid = 0
	}
	out.WriteString(fmt.Sprintf("... [%d lines suppressed]\n", mid))
	start := len(lines) - smartTail
	if start < smartHead {
		start = smartHead
	}
	for i := start; i < len(lines); i++ {
		out.WriteString(lines[i] + "\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func filterDedupLog(text string) string {
	var out bytes.Buffer
	lines := strings.Split(text, "\n")
	var prev string
	for i, line := range lines {
		if i >= dedupCap {
			break
		}
		if line == prev {
			continue
		}
		prev = line
		out.WriteString(line + "\n")
	}
	return strings.TrimRight(out.String(), "\n")
}

func isGrepLine(line string) bool {
	return reGrepLine.MatchString(line)
}

func isPathLike(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	if len(t) >= 3 && t[1] == ':' && (t[2] == '\\' || t[2] == '/') {
		return true
	}
	if strings.Contains(t, ":") {
		return false
	}
	return strings.HasPrefix(t, ".") || strings.HasPrefix(t, "/") || strings.Contains(t, "/")
}

func isMostlyPorcelain(head string) bool {
	lines := strings.Split(head, "\n")
	nonEmpty := filterEmpty(lines)
	if len(nonEmpty) < 3 {
		return false
	}
	hits := 0
	for _, l := range nonEmpty {
		if rePorcelain.MatchString(l) {
			hits++
		}
	}
	return float64(hits)/float64(len(nonEmpty)) >= 0.6
}

func isLineNumbered(lines []string) bool {
	hits := 0
	nonEmpty := 0
	sample := lines
	if len(sample) > 100 {
		sample = sample[:100]
	}
	for _, l := range sample {
		if l == "" {
			continue
		}
		nonEmpty++
		if len(l) > 2 && l[0] == ' ' && l[1] == ' ' && strings.Contains(l, "|") {
			hits++
		}
	}
	if nonEmpty < 5 {
		return false
	}
	return float64(hits)/float64(nonEmpty) >= numMinHit
}

func countMatches(text string, re *regexp.Regexp) int {
	return len(re.FindAllString(text, -1))
}

func filterEmpty(lines []string) []string {
	var out []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func allPathLike(lines []string) bool {
	for _, l := range lines {
		if !isPathLike(l) {
			return false
		}
	}
	return true
}

func safeApplyFilter(fn *detectedFilter, text string) string {
	if fn == nil || fn.fn == nil {
		return text
	}
	defer func() {
		recover()
	}()
	return fn.fn(text)
}

var _ = math.Round
