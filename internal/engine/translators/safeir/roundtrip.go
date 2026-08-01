package safeir

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"gorouter/internal/domain/engine"
	"gorouter/internal/engine/formats"
)

type SafeIRTranslator struct {
	source engine.RequestFormat
	target engine.RequestFormat
}

func NewSafeIRTranslator(source, target engine.RequestFormat) *SafeIRTranslator {
	return &SafeIRTranslator{source: source, target: target}
}
func (t *SafeIRTranslator) Source() engine.RequestFormat { return t.source }
func (t *SafeIRTranslator) Target() engine.RequestFormat { return t.target }

func (t *SafeIRTranslator) TranslateRequest(_ context.Context, req *engine.Request) (*engine.Request, error) {
	if req == nil {
		return nil, fmt.Errorf("safeir: nil request")
	}
	ir, err := ToSafeIR(req.RawBody, t.source)
	if err != nil {
		return nil, fmt.Errorf("safeir %s→%s: toSafeIR: %w", t.source, t.target, err)
	}
	ir.Model = req.Model
	ir.Stream = req.Stream
	targetBody, err := FromSafeIR(ir, t.target)
	if err != nil {
		return nil, fmt.Errorf("safeir %s→%s: fromSafeIR: %w", t.source, t.target, err)
	}
	return &engine.Request{
		ID: req.ID, Format: t.target, Model: req.Model,
		RawBody: targetBody, MappedBody: targetBody, Stream: req.Stream,
		MaxTokens: ir.MaxTokens, Temperature: ir.Temperature,
		Headers: req.Headers, UserID: req.UserID,
	}, nil
}

func (t *SafeIRTranslator) TranslateResponse(_ context.Context, resp *engine.Response) (*engine.Response, error) {
	if resp == nil {
		return nil, fmt.Errorf("safeir: nil response")
	}
	if t.source == t.target {
		return resp, nil
	}
	return nil, fmt.Errorf("%w: SafeIR cannot translate %s→%s responses; use a direct translator", ErrLossyTranslation, t.source, t.target)
}

func (t *SafeIRTranslator) TranslateStreamChunk(_ context.Context, chunk *formats.Chunk) (*formats.Chunk, error) {
	if chunk == nil {
		return nil, fmt.Errorf("safeir: nil chunk")
	}
	if t.source == t.target {
		return chunk, nil
	}
	return nil, fmt.Errorf("%w: SafeIR cannot translate %s→%s stream chunks; use a direct translator", ErrLossyTranslation, t.source, t.target)
}

func ToSafeIR(body json.RawMessage, source engine.RequestFormat) (*SafeIR, error) {
	if len(body) == 0 {
		return nil, fmt.Errorf("empty body")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	ir := &SafeIR{RawBody: body}
	switch source {
	case engine.FormatOpenAIChat, engine.FormatOpenAICompat:
		if err := extractOpenAI(raw, ir); err != nil {
			return nil, err
		}
	case engine.FormatAnthropic:
		if err := extractClaude(raw, ir); err != nil {
			return nil, err
		}
	case engine.FormatGemini:
		if err := extractGemini(raw, ir); err != nil {
			return nil, err
		}
	case engine.FormatCodexResponses:
		if err := extractCodex(raw, ir); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported source format: %s", source)
	}
	return ir, nil
}

func FromSafeIR(ir *SafeIR, target engine.RequestFormat) (json.RawMessage, error) {
	if ir == nil {
		return nil, fmt.Errorf("nil SafeIR")
	}
	switch target {
	case engine.FormatOpenAIChat, engine.FormatOpenAICompat:
		return buildOpenAI(ir)
	case engine.FormatAnthropic:
		return buildClaude(ir)
	case engine.FormatGemini:
		return buildGemini(ir)
	case engine.FormatCodexResponses:
		return buildCodex(ir)
	default:
		return nil, fmt.Errorf("unsupported target format: %s", target)
	}
}

// ---- Extract: Source → SafeIR ----

func extractOpenAI(raw map[string]json.RawMessage, ir *SafeIR) error {
	_ = json.Unmarshal(raw["model"], &ir.Model)
	if msgsRaw, ok := raw["messages"]; ok {
		var msgs []json.RawMessage
		if json.Unmarshal(msgsRaw, &msgs) == nil {
			for _, m := range msgs {
				msg, err := extractOpenAIMessage(m)
				if err != nil {
					return err
				}
				ir.Messages = append(ir.Messages, msg)
			}
		}
	}
	if temp, ok := raw["temperature"]; ok {
		var t float64
		if json.Unmarshal(temp, &t) == nil {
			ir.Temperature = &t
		}
	}
	if topP, ok := raw["top_p"]; ok {
		var t float64
		if json.Unmarshal(topP, &t) == nil {
			ir.TopP = &t
		}
	}
	if mt, ok := raw["max_tokens"]; ok {
		_ = json.Unmarshal(mt, &ir.MaxTokens)
	}
	if stop, ok := raw["stop"]; ok {
		var arr []string
		if json.Unmarshal(stop, &arr) == nil {
			ir.Stop = arr
		} else {
			var s string
			if json.Unmarshal(stop, &s) == nil {
				ir.Stop = []string{s}
			}
		}
	}
	if tc, ok := raw["tool_choice"]; ok {
		ir.ToolChoice = tc
	}
	if tools, ok := raw["tools"]; ok && len(tools) > 0 {
		var rawTools []json.RawMessage
		if json.Unmarshal(tools, &rawTools) == nil {
			for _, t := range rawTools {
				var fn struct {
					Function struct {
						Name        string          `json:"name"`
						Description string          `json:"description,omitempty"`
						Parameters  json.RawMessage `json:"parameters,omitempty"`
					} `json:"function"`
				}
				if json.Unmarshal(t, &fn) == nil && fn.Function.Name != "" {
					ir.Tools = append(ir.Tools, Tool{
						Name:        fn.Function.Name,
						Description: fn.Function.Description,
						Parameters:  fn.Function.Parameters,
					})
				}
			}
		}
	}
	if meta, ok := raw["metadata"]; ok {
		ir.Metadata = meta
	}
	return nil
}

func extractOpenAIMessage(raw json.RawMessage) (Message, error) {
	var base struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return Message{}, err
	}
	msg := Message{Role: base.Role}
	if base.Role == "system" {
		_ = json.Unmarshal(base.Content, &msg.Text)
		return msg, nil
	}
	var text string
	if err := json.Unmarshal(base.Content, &text); err == nil {
		msg.Text = text
	} else {
		var parts []json.RawMessage
		if err := json.Unmarshal(base.Content, &parts); err == nil {
			msg.RawParts = base.Content
			for _, p := range parts {
				var partType struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(p, &partType) != nil {
					continue
				}
				switch partType.Type {
				case "text":
					var tp struct {
						Text string `json:"text"`
					}
					if json.Unmarshal(p, &tp) == nil {
						msg.Text = tp.Text
					}
				case "image_url":
					var ip struct {
						ImageURL struct {
							URL    string `json:"url"`
							Detail string `json:"detail,omitempty"`
						} `json:"image_url"`
					}
					if json.Unmarshal(p, &ip) == nil {
						msg.ImageURLs = append(msg.ImageURLs, ip.ImageURL.URL)
						mime := detectMimeFromURL(ip.ImageURL.URL)
						if mime != "" {
							if msg.ImageMIMEs == nil {
								msg.ImageMIMEs = make(map[string]string)
							}
							msg.ImageMIMEs[ip.ImageURL.URL] = mime
						}
					}
				default:
					return Message{}, fmt.Errorf("%w: unsupported OpenAI content type: %s", ErrLossyTranslation, partType.Type)
				}
			}
		}
	}
	var extra struct {
		ToolCalls  []json.RawMessage `json:"tool_calls,omitempty"`
		ToolCallID string            `json:"tool_call_id,omitempty"`
	}
	if json.Unmarshal(raw, &extra) == nil {
		msg.ToolCallID = extra.ToolCallID
		for _, tc := range extra.ToolCalls {
			var tcObj struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			}
			if json.Unmarshal(tc, &tcObj) == nil {
				msg.ToolCalls = append(msg.ToolCalls, ToolCall{
					ID: tcObj.ID, Type: tcObj.Type,
					Name: tcObj.Function.Name, Arguments: tcObj.Function.Arguments,
				})
			}
		}
	}
	return msg, nil
}

func extractClaude(raw map[string]json.RawMessage, ir *SafeIR) error {
	_ = json.Unmarshal(raw["model"], &ir.Model)
	if sys, ok := raw["system"]; ok {
		_ = json.Unmarshal(sys, &ir.System)
	}
	if msgsRaw, ok := raw["messages"]; ok {
		var msgs []json.RawMessage
		if json.Unmarshal(msgsRaw, &msgs) == nil {
			for _, m := range msgs {
				msg, err := extractClaudeMessage(m)
				if err != nil {
					return err
				}
				ir.Messages = append(ir.Messages, msg)
			}
		}
	}
	if temp, ok := raw["temperature"]; ok {
		var t float64
		if json.Unmarshal(temp, &t) == nil {
			ir.Temperature = &t
		}
	}
	if mt, ok := raw["max_tokens"]; ok {
		_ = json.Unmarshal(mt, &ir.MaxTokens)
	}
	if ss, ok := raw["stop_sequences"]; ok {
		_ = json.Unmarshal(ss, &ir.Stop)
	}
	if rawTools, ok := raw["tools"]; ok && len(rawTools) > 0 {
		var tools []json.RawMessage
		if json.Unmarshal(rawTools, &tools) == nil {
			for _, t := range tools {
				var tool struct {
					Name        string          `json:"name"`
					Description string          `json:"description,omitempty"`
					InputSchema json.RawMessage `json:"input_schema,omitempty"`
				}
				if json.Unmarshal(t, &tool) == nil && tool.Name != "" {
					ir.Tools = append(ir.Tools, Tool{
						Name: tool.Name, Description: tool.Description,
						Parameters: tool.InputSchema,
					})
				}
			}
		}
	}
	if tc, ok := raw["tool_choice"]; ok {
		ir.ToolChoice = tc
	}
	return nil
}

func extractClaudeMessage(raw json.RawMessage) (Message, error) {
	var base struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &base); err != nil {
		return Message{}, err
	}
	msg := Message{Role: base.Role}
	var text string
	if err := json.Unmarshal(base.Content, &text); err == nil {
		msg.Text = text
		return msg, nil
	}
	msg.RawParts = base.Content
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(base.Content, &blocks); err != nil {
		return msg, nil
	}
	for _, block := range blocks {
		var blockType string
		if json.Unmarshal(block["type"], &blockType) != nil {
			continue
		}
		switch blockType {
		case "text":
			_ = json.Unmarshal(block["text"], &msg.Text)
		case "thinking":
			_ = json.Unmarshal(block["thinking"], &msg.Thinking)
		case "tool_use":
			var id, name string
			_ = json.Unmarshal(block["id"], &id)
			_ = json.Unmarshal(block["name"], &name)
			var input json.RawMessage
			if raw, ok := block["input"]; ok {
				var buf bytes.Buffer
				if json.Compact(&buf, raw) == nil {
					input = buf.Bytes()
				} else {
					input = raw
				}
			}
			msg.ToolCalls = append(msg.ToolCalls, ToolCall{ID: id, Type: "function", Name: name, Arguments: input})
		case "tool_result":
			_ = json.Unmarshal(block["tool_use_id"], &msg.ToolCallID)
			if c, ok := block["content"]; ok {
				_ = json.Unmarshal(c, &msg.Text)
			}
		case "image":
			if src, ok := block["source"]; ok {
				var srcObj struct {
					Data      string `json:"data"`
					MediaType string `json:"media_type,omitempty"`
				}
				if json.Unmarshal(src, &srcObj) == nil {
					msg.ImageURLs = append(msg.ImageURLs, srcObj.Data)
					if srcObj.MediaType != "" {
						if msg.ImageMIMEs == nil {
							msg.ImageMIMEs = make(map[string]string)
						}
						msg.ImageMIMEs[srcObj.Data] = srcObj.MediaType
					}
				}
			}
		default:
			return Message{}, fmt.Errorf("%w: unsupported Claude content block: %s", ErrLossyTranslation, blockType)
		}
	}
	return msg, nil
}

func extractGemini(raw map[string]json.RawMessage, ir *SafeIR) error {
	if sys, ok := raw["systemInstruction"]; ok {
		var si struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		}
		if json.Unmarshal(sys, &si) == nil {
			for _, p := range si.Parts {
				ir.System += p.Text
			}
		}
	}
	if contents, ok := raw["contents"]; ok {
		var contentList []json.RawMessage
		if json.Unmarshal(contents, &contentList) == nil {
			for _, c := range contentList {
				msg, err := extractGeminiContent(c)
				if err != nil {
					return err
				}
				ir.Messages = append(ir.Messages, msg)
			}
		}
	}
	if gc, ok := raw["generationConfig"]; ok {
		var cfg struct {
			Temperature   *float64 `json:"temperature,omitempty"`
			TopP          *float64 `json:"topP,omitempty"`
			MaxOutTokens  int      `json:"maxOutputTokens,omitempty"`
			StopSequences []string `json:"stopSequences,omitempty"`
		}
		if json.Unmarshal(gc, &cfg) == nil {
			ir.Temperature = cfg.Temperature
			ir.TopP = cfg.TopP
			ir.MaxTokens = cfg.MaxOutTokens
			ir.Stop = cfg.StopSequences
		}
	}
	if rawTools, ok := raw["tools"]; ok && len(rawTools) > 0 {
		var tools []json.RawMessage
		if json.Unmarshal(rawTools, &tools) == nil {
			for _, t := range tools {
				var entry struct {
					FunctionDeclarations []json.RawMessage `json:"functionDeclarations"`
				}
				if json.Unmarshal(t, &entry) != nil {
					continue
				}
				for _, fd := range entry.FunctionDeclarations {
					var decl struct {
						Name        string          `json:"name"`
						Description string          `json:"description,omitempty"`
						Parameters  json.RawMessage `json:"parameters,omitempty"`
					}
					if json.Unmarshal(fd, &decl) == nil && decl.Name != "" {
						ir.Tools = append(ir.Tools, Tool{
							Name: decl.Name, Description: decl.Description,
							Parameters: decl.Parameters,
						})
					}
				}
			}
		}
	}
	return nil
}

func extractGeminiContent(raw json.RawMessage) (Message, error) {
	var c struct {
		Role  string            `json:"role"`
		Parts []json.RawMessage `json:"parts"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return Message{}, err
	}
	role := "user"
	if c.Role == "model" {
		role = "assistant"
	} else if c.Role == "function" {
		role = "tool"
	}
	msg := Message{Role: role}
	for _, part := range c.Parts {
		var p struct {
			Text             string          `json:"text,omitempty"`
			InlineData       json.RawMessage `json:"inlineData,omitempty"`
			FunctionCall     json.RawMessage `json:"functionCall,omitempty"`
			FunctionResponse json.RawMessage `json:"functionResponse,omitempty"`
		}
		if err := json.Unmarshal(part, &p); err != nil {
			continue
		}
		switch {
		case p.Text != "":
			msg.Text += p.Text
		case p.InlineData != nil:
			var id struct {
				MimeType string `json:"mimeType"`
				Data     string `json:"data"`
			}
			if json.Unmarshal(p.InlineData, &id) == nil {
				msg.ImageURLs = append(msg.ImageURLs, id.Data)
				if id.MimeType != "" {
					if msg.ImageMIMEs == nil {
						msg.ImageMIMEs = make(map[string]string)
					}
					msg.ImageMIMEs[id.Data] = id.MimeType
				}
			}
		case p.FunctionCall != nil:
			return Message{}, fmt.Errorf("%w: Gemini functionCall requires direct translator", ErrLossyTranslation)
		case p.FunctionResponse != nil:
			return Message{}, fmt.Errorf("%w: Gemini functionResponse requires direct translator", ErrLossyTranslation)
		default:
			return Message{}, fmt.Errorf("%w: unsupported Gemini part type", ErrLossyTranslation)
		}
	}
	return msg, nil
}

func extractCodex(raw map[string]json.RawMessage, ir *SafeIR) error {
	_ = json.Unmarshal(raw["model"], &ir.Model)
	if input, ok := raw["input"]; ok {
		var str string
		if json.Unmarshal(input, &str) == nil {
			ir.Messages = []Message{{Role: "user", Text: str}}
		} else {
			var items []json.RawMessage
			if json.Unmarshal(input, &items) == nil {
				for _, item := range items {
					var s string
					if json.Unmarshal(item, &s) == nil {
						ir.Messages = append(ir.Messages, Message{Role: "user", Text: s})
						continue
					}
					var msgObj struct {
						Role    string          `json:"role"`
						Content json.RawMessage `json:"content"`
					}
					if json.Unmarshal(item, &msgObj) == nil {
						var txt string
						_ = json.Unmarshal(msgObj.Content, &txt)
						ir.Messages = append(ir.Messages, Message{Role: msgObj.Role, Text: txt})
					}
				}
			}
		}
	}
	if instr, ok := raw["instructions"]; ok {
		_ = json.Unmarshal(instr, &ir.System)
	}
	if mt, ok := raw["max_output_tokens"]; ok {
		_ = json.Unmarshal(mt, &ir.MaxTokens)
	}
	if temp, ok := raw["temperature"]; ok {
		var t float64
		if json.Unmarshal(temp, &t) == nil {
			ir.Temperature = &t
		}
	}
	if rawTools, ok := raw["tools"]; ok && len(rawTools) > 0 {
		var tools []json.RawMessage
		if json.Unmarshal(rawTools, &tools) == nil {
			for _, t := range tools {
				var fn struct {
					Function struct {
						Name        string          `json:"name"`
						Description string          `json:"description,omitempty"`
						Parameters  json.RawMessage `json:"parameters,omitempty"`
					} `json:"function"`
				}
				if json.Unmarshal(t, &fn) == nil && fn.Function.Name != "" {
					ir.Tools = append(ir.Tools, Tool{
						Name: fn.Function.Name, Description: fn.Function.Description,
						Parameters: fn.Function.Parameters,
					})
				}
			}
		}
	}
	if tc, ok := raw["tool_choice"]; ok {
		ir.ToolChoice = tc
	}
	return nil
}

// ---- Build: SafeIR → Target ----

func buildOpenAI(ir *SafeIR) (json.RawMessage, error) {
	msgs, err := buildOpenAIMessages(ir)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{"model": ir.Model, "messages": msgs}
	if ir.Temperature != nil {
		out["temperature"] = *ir.Temperature
	}
	if ir.TopP != nil {
		out["top_p"] = *ir.TopP
	}
	if ir.MaxTokens > 0 {
		out["max_tokens"] = ir.MaxTokens
	}
	if len(ir.Stop) > 0 {
		out["stop"] = ir.Stop
	}
	if ir.ToolChoice != nil {
		if raw, ok := ir.ToolChoice.(json.RawMessage); !ok || len(raw) > 0 {
			out["tool_choice"] = ir.ToolChoice
		}
	}
	if len(ir.Tools) > 0 {
		openaiTools := make([]map[string]interface{}, 0, len(ir.Tools))
		for _, t := range ir.Tools {
			openaiTools = append(openaiTools, map[string]interface{}{
				"type": "function",
				"function": map[string]interface{}{
					"name":        t.Name,
					"description": t.Description,
					"parameters":  t.Parameters,
				},
			})
		}
		out["tools"] = openaiTools
	}
	if len(ir.Metadata) > 0 {
		out["metadata"] = ir.Metadata
	}
	return json.Marshal(out)
}

func buildOpenAIMessages(ir *SafeIR) ([]json.RawMessage, error) {
	msgs := make([]json.RawMessage, 0)
	if ir.System != "" {
		s, _ := json.Marshal(map[string]interface{}{"role": "system", "content": ir.System})
		msgs = append(msgs, s)
	}
	for _, m := range ir.Messages {
		if m.Thinking != "" {
			return nil, fmt.Errorf("%w: thinking tokens not representable in OpenAI Chat via SafeIR", ErrLossyTranslation)
		}
		openaiMsg := map[string]interface{}{"role": m.Role}
		if len(m.ToolCalls) > 0 {
			openaiMsg["content"] = nil
			tcs := make([]map[string]interface{}, 0)
			for _, tc := range m.ToolCalls {
				argsStr, _ := json.Marshal(tc.Arguments)
				tcs = append(tcs, map[string]interface{}{
					"id": tc.ID, "type": "function",
					"function": map[string]string{"name": tc.Name, "arguments": string(argsStr)},
				})
			}
			openaiMsg["tool_calls"] = tcs
		} else if m.ToolCallID != "" {
			openaiMsg["role"] = "tool"
			openaiMsg["tool_call_id"] = m.ToolCallID
			openaiMsg["content"] = m.Text
		} else if len(m.ImageURLs) > 0 {
			content := make([]map[string]interface{}, 0)
			if m.Text != "" {
				content = append(content, map[string]interface{}{"type": "text", "text": m.Text})
			}
			for _, url := range m.ImageURLs {
				mime := m.ImageMIMEs[url]
				if mime == "" {
					mime = detectMimeFromURL(url)
				}
				content = append(content, map[string]interface{}{
					"type":      "image_url",
					"image_url": map[string]string{"url": url, "detail": "auto"},
				})
			}
			cb, _ := json.Marshal(content)
			openaiMsg["content"] = json.RawMessage(cb)
		} else {
			openaiMsg["content"] = m.Text
		}
		mb, _ := json.Marshal(openaiMsg)
		msgs = append(msgs, mb)
	}
	return msgs, nil
}

func buildClaude(ir *SafeIR) (json.RawMessage, error) {
	msgs, err := buildClaudeMessages(ir)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{"model": ir.Model, "max_tokens": ir.MaxTokens, "messages": msgs}
	if ir.System != "" {
		out["system"] = ir.System
	}
	if ir.Temperature != nil {
		out["temperature"] = *ir.Temperature
	}
	if ir.TopP != nil {
		out["top_p"] = *ir.TopP
	}
	if len(ir.Stop) > 0 {
		out["stop_sequences"] = ir.Stop
	}
	if len(ir.Tools) > 0 {
		claudeTools := make([]map[string]interface{}, 0, len(ir.Tools))
		for _, t := range ir.Tools {
			ct := map[string]interface{}{"name": t.Name, "description": t.Description}
			if len(t.Parameters) > 0 {
				ct["input_schema"] = t.Parameters
			}
			claudeTools = append(claudeTools, ct)
		}
		out["tools"] = claudeTools
	}
	if ir.ToolChoice != nil {
		var tcStr string
		if v, ok := ir.ToolChoice.(string); ok {
			tcStr = v
		} else {
			raw, _ := json.Marshal(ir.ToolChoice)
			_ = json.Unmarshal(raw, &tcStr)
		}
		switch tcStr {
		case "auto":
			out["tool_choice"] = map[string]string{"type": "auto"}
		case "required", "any":
			out["tool_choice"] = map[string]string{"type": "any"}
		case "none":
			out["tool_choice"] = map[string]string{"type": "none"}
		default:
			out["tool_choice"] = map[string]string{"type": "auto"}
		}
	}
	return json.Marshal(out)
}

func buildClaudeMessages(ir *SafeIR) ([]json.RawMessage, error) {
	msgs := make([]json.RawMessage, 0)
	for _, m := range ir.Messages {
		claudeMsg := map[string]interface{}{"role": claudeRole(m.Role)}
		switch {
		case len(m.ToolCalls) > 0:
			blocks := make([]map[string]interface{}, 0)
			if m.Text != "" {
				blocks = append(blocks, map[string]interface{}{"type": "text", "text": m.Text})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, map[string]interface{}{"type": "tool_use", "id": tc.ID, "name": tc.Name, "input": tc.Arguments})
			}
			cb, _ := json.Marshal(blocks)
			claudeMsg["content"] = json.RawMessage(cb)
		case m.ToolCallID != "":
			blocks := []map[string]interface{}{{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Text}}
			cb, _ := json.Marshal(blocks)
			claudeMsg["content"] = json.RawMessage(cb)
		case len(m.ImageURLs) > 0:
			blocks := make([]map[string]interface{}, 0)
			if m.Text != "" {
				blocks = append(blocks, map[string]interface{}{"type": "text", "text": m.Text})
			}
			for _, url := range m.ImageURLs {
				mime := m.ImageMIMEs[url]
				if mime == "" {
					mime = detectMimeFromURL(url)
				}
				blocks = append(blocks, map[string]interface{}{
					"type":   "image",
					"source": map[string]string{"type": "base64", "media_type": mime, "data": url},
				})
			}
			cb, _ := json.Marshal(blocks)
			claudeMsg["content"] = json.RawMessage(cb)
		case m.Thinking != "":
			blocks := []map[string]interface{}{{"type": "thinking", "thinking": m.Thinking}}
			if m.Text != "" {
				blocks = append(blocks, map[string]interface{}{"type": "text", "text": m.Text})
			}
			cb, _ := json.Marshal(blocks)
			claudeMsg["content"] = json.RawMessage(cb)
		default:
			claudeMsg["content"] = m.Text
		}
		mb, _ := json.Marshal(claudeMsg)
		msgs = append(msgs, mb)
	}
	return msgs, nil
}

func claudeRole(role string) string {
	switch role {
	case "assistant":
		return "assistant"
	case "tool":
		return "user"
	default:
		return "user"
	}
}

func buildGemini(ir *SafeIR) (json.RawMessage, error) {
	if len(ir.Tools) > 0 || ir.ToolChoice != nil {
		return nil, fmt.Errorf("%w: Gemini tools/tool_choice not representable via SafeIR", ErrLossyTranslation)
	}
	contents, err := buildGeminiContents(ir)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{"contents": contents}
	if ir.System != "" {
		out["systemInstruction"] = map[string]interface{}{
			"parts": []map[string]string{{"text": ir.System}},
		}
	}
	gc := map[string]interface{}{}
	if ir.Temperature != nil {
		gc["temperature"] = *ir.Temperature
	}
	if ir.TopP != nil {
		gc["topP"] = *ir.TopP
	}
	if ir.MaxTokens > 0 {
		gc["maxOutputTokens"] = ir.MaxTokens
	}
	if len(ir.Stop) > 0 {
		gc["stopSequences"] = ir.Stop
	}
	if len(gc) > 0 {
		out["generationConfig"] = gc
	}
	return json.Marshal(out)
}

func buildGeminiContents(ir *SafeIR) ([]map[string]interface{}, error) {
	contents := make([]map[string]interface{}, 0)
	for _, m := range ir.Messages {
		if m.Thinking != "" {
			return nil, fmt.Errorf("%w: thinking tokens not representable in Gemini via SafeIR", ErrLossyTranslation)
		}
		if len(m.ToolCalls) > 0 {
			return nil, fmt.Errorf("%w: tool calls not representable in Gemini via SafeIR", ErrLossyTranslation)
		}
		role := "user"
		if m.Role == "assistant" || m.Role == "model" {
			role = "model"
		}
		parts := make([]map[string]interface{}, 0)
		if m.Text != "" {
			parts = append(parts, map[string]interface{}{"text": m.Text})
		}
		for _, url := range m.ImageURLs {
			mime := m.ImageMIMEs[url]
			if mime == "" {
				mime = detectMimeFromURL(url)
			}
			parts = append(parts, map[string]interface{}{
				"inlineData": map[string]string{"mimeType": mime, "data": url},
			})
		}
		contents = append(contents, map[string]interface{}{"role": role, "parts": parts})
	}
	return contents, nil
}

func buildCodex(ir *SafeIR) (json.RawMessage, error) {
	if len(ir.Tools) > 0 || ir.ToolChoice != nil {
		return nil, fmt.Errorf("%w: Codex format cannot represent tools or tool_choice via SafeIR", ErrLossyTranslation)
	}
	if hasLossyContent(ir) {
		return nil, fmt.Errorf("%w: Codex format cannot represent tool calls, thinking, or images via SafeIR", ErrLossyTranslation)
	}
	out := map[string]interface{}{"model": ir.Model, "input": buildCodexInput(ir)}
	if ir.System != "" {
		out["instructions"] = ir.System
	}
	if ir.Temperature != nil {
		out["temperature"] = *ir.Temperature
	}
	if ir.MaxTokens > 0 {
		out["max_output_tokens"] = ir.MaxTokens
	}
	return json.Marshal(out)
}

func hasLossyContent(ir *SafeIR) bool {
	for _, m := range ir.Messages {
		if len(m.ToolCalls) > 0 || m.Thinking != "" || len(m.ImageURLs) > 0 {
			return true
		}
	}
	return false
}

func buildCodexInput(ir *SafeIR) interface{} {
	if len(ir.Messages) == 1 && ir.Messages[0].Role == "user" && ir.Messages[0].Text != "" {
		return ir.Messages[0].Text
	}
	items := make([]map[string]interface{}, 0)
	for _, m := range ir.Messages {
		item := map[string]interface{}{"role": m.Role, "content": m.Text}
		if m.ToolCallID != "" {
			item["tool_call_id"] = m.ToolCallID
		}
		if len(m.ToolCalls) > 0 {
			tcs := make([]map[string]interface{}, 0)
			for _, tc := range m.ToolCalls {
				argsStr, _ := json.Marshal(tc.Arguments)
				tcs = append(tcs, map[string]interface{}{
					"id": tc.ID, "type": "function",
					"function": map[string]string{"name": tc.Name, "arguments": string(argsStr)},
				})
			}
			item["tool_calls"] = tcs
		}
		items = append(items, item)
	}
	return items
}

func detectMimeFromURL(url string) string {
	lower := strings.ToLower(url)
	switch {
	case strings.HasPrefix(lower, "data:image/png"):
		return "image/png"
	case strings.HasPrefix(lower, "data:image/jpeg"), strings.HasPrefix(lower, "data:image/jpg"):
		return "image/jpeg"
	case strings.HasPrefix(lower, "data:image/webp"):
		return "image/webp"
	case strings.HasPrefix(lower, "data:image/gif"):
		return "image/gif"
	case strings.HasPrefix(lower, "data:image/"):
		// Extract from data URI.
		parts := strings.SplitN(lower, ";", 2)
		if len(parts) > 0 {
			mime := strings.TrimPrefix(parts[0], "data:")
			if mime != "" {
				return mime
			}
		}
		return "image/jpeg"
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".jpg"), strings.HasSuffix(lower, ".jpeg"):
		return "image/jpeg"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	default:
		return "image/jpeg"
	}
}
