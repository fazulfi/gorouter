package mutators

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gorouter/internal/domain/engine"
)

type HeadroomConfig struct {
	Enabled          bool
	URL              string
	Model            string
	TimeoutMs        int
	CompressUserMsgs bool
}

type HeadroomStats struct {
	TokensBefore int `json:"tokens_before"`
	TokensAfter  int `json:"tokens_after"`
	TokensSaved  int `json:"tokens_saved"`
	BodyBefore   int `json:"body_before"`
	BodyAfter    int `json:"body_after"`
}

type headroomMutator struct {
	cfg HeadroomConfig
}

func NewHeadroom(cfg HeadroomConfig) Mutator {
	return &headroomMutator{cfg: cfg}
}

func (m *headroomMutator) Name() string { return "headroom" }

func (m *headroomMutator) Mutate(ctx context.Context, req *engine.Request) (*Result, error) {
	if !m.cfg.Enabled {
		return &Result{Applied: false}, nil
	}
	if req == nil || len(req.MappedBody) == 0 {
		return &Result{Applied: false}, nil
	}
	if m.cfg.URL == "" {
		return &Result{Applied: false}, nil
	}

	body := make(map[string]json.RawMessage)
	if err := json.Unmarshal(req.MappedBody, &body); err != nil {
		return &Result{Applied: false}, nil
	}

	msgs := extractMessages(body)
	if len(msgs) == 0 {
		return &Result{Applied: false}, nil
	}

	var msgList []map[string]interface{}
	if err := json.Unmarshal(serializeMessages(msgs), &msgList); err != nil {
		return &Result{Applied: false}, nil
	}

	timeout := m.cfg.TimeoutMs
	if timeout <= 0 {
		timeout = 3000
	}
	model := m.cfg.Model
	if model == "" {
		model = req.Model
	}

	compressed, err := callHeadroom(ctx, m.cfg.URL, msgList, model, timeout, m.cfg.CompressUserMsgs)
	if err != nil {
		// fail-open: leave request unchanged
		return &Result{Applied: false}, nil
	}

	if len(compressed) == 0 {
		return &Result{Applied: false}, nil
	}

	beforeBytes := len(req.MappedBody)

	// Update body with compressed messages
	compressedRaw, _ := json.Marshal(compressed)
	if _, ok := body["messages"]; ok {
		body["messages"] = compressedRaw
	} else if _, ok := body["input"]; ok {
		body["input"] = compressedRaw
	}

	updated, err := json.Marshal(body)
	if err != nil {
		return &Result{Applied: false}, nil
	}
	req.MappedBody = updated

	afterBytes := len(updated)

	return &Result{
		Applied: true,
		Stats: map[string]interface{}{
			"body_before":  beforeBytes,
			"body_after":   afterBytes,
			"tokens_saved": (beforeBytes - afterBytes) / 4,
		},
	}, nil
}

func callHeadroom(ctx context.Context, url string, messages []map[string]interface{}, model string, timeoutMs int, compressUserMsgs bool) ([]map[string]interface{}, error) {
	endpoint := buildCompressEndpoint(url)
	payload := map[string]interface{}{
		"messages": messages,
		"model":    model,
	}
	if compressUserMsgs {
		payload["config"] = map[string]interface{}{
			"compress_user_messages": true,
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("proxy returned HTTP %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Messages []map[string]interface{} `json:"messages"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}
	if len(result.Messages) == 0 {
		return nil, fmt.Errorf("proxy returned empty messages")
	}
	return result.Messages, nil
}

func buildCompressEndpoint(url string) string {
	url = strings.TrimRight(url, "/")
	if !strings.Contains(url, "/v1/compress") {
		url += "/v1/compress"
	}
	return url
}

func serializeMessages(msgs []json.RawMessage) []byte {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, m := range msgs {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(m)
	}
	buf.WriteByte(']')
	return buf.Bytes()
}
