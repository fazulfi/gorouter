package direct

import (
	"encoding/json"
	"fmt"
)

// ---- OpenAI→Claude request helpers ----

func openaiUserToClaude(rawMsg json.RawMessage) (json.RawMessage, error) {
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		return nil, err
	}
	var text string
	if err := json.Unmarshal(msg.Content, &text); err == nil {
		return json.Marshal(map[string]interface{}{"role": "user", "content": text})
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(msg.Content, &parts); err != nil {
		return nil, fmt.Errorf("content must be string or array: %w", err)
	}
	blocks := make([]map[string]interface{}, 0, len(parts))
	for _, part := range parts {
		var pt struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(part, &pt) != nil {
			continue
		}
		switch pt.Type {
		case "text":
			var tb struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(part, &tb) == nil {
				blocks = append(blocks, map[string]interface{}{"type": "text", "text": tb.Text})
			}
		case "image_url":
			var ib struct {
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			}
			if json.Unmarshal(part, &ib) == nil {
				blocks = append(blocks, map[string]interface{}{
					"type": "image",
					"source": map[string]interface{}{
						"type": "base64", "media_type": detectImageMIME(ib.ImageURL.URL), "data": ib.ImageURL.URL,
					},
				})
			}
		}
	}
	return json.Marshal(map[string]interface{}{"role": "user", "content": blocks})
}

func openaiAssistantToClaude(rawMsg json.RawMessage) (json.RawMessage, error) {
	var msg struct {
		Content   json.RawMessage   `json:"content"`
		ToolCalls []json.RawMessage `json:"tool_calls,omitempty"`
	}
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		return nil, err
	}
	blocks := make([]map[string]interface{}, 0)
	if len(msg.Content) > 0 {
		var text string
		if json.Unmarshal(msg.Content, &text) == nil && text != "" {
			blocks = append(blocks, map[string]interface{}{"type": "text", "text": text})
		} else {
			var parts []json.RawMessage
			if json.Unmarshal(msg.Content, &parts) == nil {
				for _, part := range parts {
					var pt struct {
						Type string `json:"type"`
					}
					if json.Unmarshal(part, &pt) != nil {
						continue
					}
					if pt.Type == "text" {
						var tb struct {
							Text string `json:"text"`
						}
						if json.Unmarshal(part, &tb) == nil {
							blocks = append(blocks, map[string]interface{}{"type": "text", "text": tb.Text})
						}
					}
				}
			}
		}
	}
	for _, tc := range msg.ToolCalls {
		var tcObj struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		}
		if json.Unmarshal(tc, &tcObj) != nil {
			continue
		}
		var args json.RawMessage
		json.Unmarshal([]byte(tcObj.Function.Arguments), &args)
		blocks = append(blocks, map[string]interface{}{"type": "tool_use", "id": tcObj.ID, "name": tcObj.Function.Name, "input": args})
	}
	return json.Marshal(map[string]interface{}{"role": "assistant", "content": blocks})
}

func openaiToolToClaude(rawMsg json.RawMessage) (json.RawMessage, error) {
	var msg struct {
		ToolCallID string          `json:"tool_call_id"`
		Content    json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		return nil, err
	}
	var contentStr string
	json.Unmarshal(msg.Content, &contentStr)
	blocks := []map[string]interface{}{
		{"type": "tool_result", "tool_use_id": msg.ToolCallID, "content": contentStr},
	}
	return json.Marshal(map[string]interface{}{"role": "user", "content": blocks})
}

func openaiToolsToClaude(openaiTools []json.RawMessage) []map[string]interface{} {
	result := make([]map[string]interface{}, 0)
	for _, t := range openaiTools {
		var tool struct {
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description,omitempty"`
				Parameters  json.RawMessage `json:"parameters,omitempty"`
			} `json:"function"`
		}
		if json.Unmarshal(t, &tool) != nil || tool.Function.Name == "" {
			continue
		}
		ct := map[string]interface{}{"name": tool.Function.Name, "description": tool.Function.Description}
		if len(tool.Function.Parameters) > 0 {
			ct["input_schema"] = tool.Function.Parameters
		}
		result = append(result, ct)
	}
	return result
}

func mapToolChoice(tc json.RawMessage) interface{} {
	var str string
	if json.Unmarshal(tc, &str) == nil {
		switch str {
		case "auto":
			return map[string]string{"type": "auto"}
		case "required", "any":
			return map[string]string{"type": "any"}
		case "none":
			return map[string]string{"type": "none"}
		}
		return map[string]string{"type": "auto"}
	}
	var specific struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(tc, &specific) == nil && specific.Function.Name != "" {
		return map[string]interface{}{"type": "tool", "name": specific.Function.Name}
	}
	return map[string]string{"type": "auto"}
}

// ---- Claude→OpenAI request helpers ----

func claudeUserToOpenAI(rawMsg json.RawMessage) (json.RawMessage, error) {
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		return nil, err
	}
	var text string
	if json.Unmarshal(msg.Content, &text) == nil {
		return json.Marshal(map[string]interface{}{"role": "user", "content": text})
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil, fmt.Errorf("content must be string or array: %w", err)
	}
	for _, b := range blocks {
		var t string
		if json.Unmarshal(b["type"], &t) == nil && t == "tool_result" {
			return claudeToolResultToOpenAI(blocks)
		}
	}
	parts := make([]map[string]interface{}, 0)
	for _, b := range blocks {
		var t string
		if json.Unmarshal(b["type"], &t) != nil {
			continue
		}
		switch t {
		case "text":
			var txt string
			if json.Unmarshal(b["text"], &txt) == nil {
				parts = append(parts, map[string]interface{}{"type": "text", "text": txt})
			}
		case "image":
			var src struct {
				Data string `json:"data"`
			}
			if s, ok := b["source"]; ok {
				srcBytes, _ := s.MarshalJSON()
				json.Unmarshal(srcBytes, &src)
			}
			parts = append(parts, map[string]interface{}{
				"type":      "image_url",
				"image_url": map[string]string{"url": src.Data},
			})
		}
	}
	if len(parts) == 1 && parts[0]["type"] == "text" {
		return json.Marshal(map[string]interface{}{"role": "user", "content": parts[0]["text"]})
	}
	return json.Marshal(map[string]interface{}{"role": "user", "content": parts})
}

func claudeToolResultToOpenAI(blocks []map[string]json.RawMessage) (json.RawMessage, error) {
	var toolUseID, contentStr string
	for _, b := range blocks {
		var t string
		if json.Unmarshal(b["type"], &t) != nil {
			continue
		}
		if t == "tool_result" {
			json.Unmarshal(b["tool_use_id"], &toolUseID)
			if c, ok := b["content"]; ok {
				json.Unmarshal(c, &contentStr)
			}
		}
	}
	return json.Marshal(map[string]interface{}{
		"role": "tool", "tool_call_id": toolUseID, "content": contentStr,
	})
}

func claudeAssistantToOpenAI(rawMsg json.RawMessage) (json.RawMessage, error) {
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		return nil, err
	}
	var text string
	if json.Unmarshal(msg.Content, &text) == nil {
		return json.Marshal(map[string]interface{}{"role": "assistant", "content": text})
	}
	var blocks []map[string]json.RawMessage
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return nil, fmt.Errorf("assistant content must be string or array: %w", err)
	}
	toolCalls := make([]map[string]interface{}, 0)
	contentParts := make([]string, 0)
	for _, b := range blocks {
		var t string
		if json.Unmarshal(b["type"], &t) != nil {
			continue
		}
		switch t {
		case "text":
			var txt string
			if json.Unmarshal(b["text"], &txt) == nil {
				contentParts = append(contentParts, txt)
			}
		case "thinking":
			var th string
			if json.Unmarshal(b["thinking"], &th) == nil {
				contentParts = append(contentParts, "[thinking] "+th)
			}
		case "tool_use":
			var id, name string
			json.Unmarshal(b["id"], &id)
			json.Unmarshal(b["name"], &name)
			var input []byte
			if inp, ok := b["input"]; ok {
				input, _ = json.Marshal(inp)
			}
			toolCalls = append(toolCalls, map[string]interface{}{
				"id": id, "type": "function",
				"function": map[string]string{"name": name, "arguments": string(input)},
			})
		}
	}
	content := ""
	for _, p := range contentParts {
		content += p
	}
	m := map[string]interface{}{"role": "assistant"}
	if content != "" {
		m["content"] = content
	} else {
		m["content"] = nil
	}
	if len(toolCalls) > 0 {
		m["tool_calls"] = toolCalls
	}
	return json.Marshal(m)
}

func claudeToolsToOpenAI(claudeTools []json.RawMessage) []map[string]interface{} {
	result := make([]map[string]interface{}, 0)
	for _, t := range claudeTools {
		var tool struct {
			Name        string          `json:"name"`
			Description string          `json:"description,omitempty"`
			InputSchema json.RawMessage `json:"input_schema,omitempty"`
		}
		if json.Unmarshal(t, &tool) != nil || tool.Name == "" {
			continue
		}
		ot := map[string]interface{}{
			"type":     "function",
			"function": map[string]interface{}{"name": tool.Name, "description": tool.Description},
		}
		if len(tool.InputSchema) > 0 {
			ot["function"].(map[string]interface{})["parameters"] = tool.InputSchema
		}
		result = append(result, ot)
	}
	return result
}

func mapClaudeToolChoice(tc json.RawMessage) interface{} {
	var parsed struct {
		Type string `json:"type"`
		Name string `json:"name,omitempty"`
	}
	if json.Unmarshal(tc, &parsed) != nil {
		return "auto"
	}
	switch parsed.Type {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "none":
		return "none"
	case "tool":
		if parsed.Name != "" {
			return map[string]interface{}{"type": "function", "function": map[string]string{"name": parsed.Name}}
		}
		return "required"
	default:
		return "auto"
	}
}

// ---- OpenAI→Gemini request helpers ----

func openaiContentToGeminiParts(content json.RawMessage) ([]map[string]interface{}, error) {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return []map[string]interface{}{{"text": text}}, nil
	}
	var parts []json.RawMessage
	if err := json.Unmarshal(content, &parts); err != nil {
		return nil, fmt.Errorf("content is not string or array: %w", err)
	}
	result := make([]map[string]interface{}, 0, len(parts))
	for _, p := range parts {
		var pt struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(p, &pt) != nil {
			continue
		}
		switch pt.Type {
		case "text":
			var tb struct {
				Text string `json:"text"`
			}
			if json.Unmarshal(p, &tb) == nil {
				result = append(result, map[string]interface{}{"text": tb.Text})
			}
		case "image_url":
			var ib struct {
				ImageURL struct {
					URL string `json:"url"`
				} `json:"image_url"`
			}
			if json.Unmarshal(p, &ib) == nil {
				result = append(result, map[string]interface{}{
					"inlineData": map[string]string{"mimeType": detectImageMIME(ib.ImageURL.URL), "data": ib.ImageURL.URL},
				})
			}
		}
	}
	return result, nil
}

func openaiAsstToGeminiParts(rawMsg json.RawMessage) (map[string]interface{}, error) {
	var msg struct {
		Content   json.RawMessage   `json:"content"`
		ToolCalls []json.RawMessage `json:"tool_calls,omitempty"`
	}
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		return nil, err
	}
	parts := make([]map[string]interface{}, 0)
	if len(msg.Content) > 0 {
		cp, _ := openaiContentToGeminiParts(msg.Content)
		parts = append(parts, cp...)
	}
	for _, tc := range msg.ToolCalls {
		var tcObj struct {
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		}
		if json.Unmarshal(tc, &tcObj) != nil {
			continue
		}
		var args map[string]interface{}
		json.Unmarshal([]byte(tcObj.Function.Arguments), &args)
		parts = append(parts, map[string]interface{}{
			"functionCall": map[string]interface{}{"name": tcObj.Function.Name, "args": args},
		})
	}
	return map[string]interface{}{"role": "model", "parts": parts}, nil
}

func openaiToolToGeminiParts(rawMsg json.RawMessage) (map[string]interface{}, error) {
	var msg struct {
		ToolCallID string          `json:"tool_call_id"`
		Content    json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(rawMsg, &msg); err != nil {
		return nil, err
	}
	var contentStr string
	json.Unmarshal(msg.Content, &contentStr)
	return map[string]interface{}{
		"role": "function",
		"parts": []map[string]interface{}{
			{"functionResponse": map[string]interface{}{
				"name":     msg.ToolCallID,
				"response": map[string]interface{}{"name": msg.ToolCallID, "content": contentStr},
			}},
		},
	}, nil
}

func openaiToolsToGemini(openaiTools []json.RawMessage) []map[string]interface{} {
	result := make([]map[string]interface{}, 0)
	for _, t := range openaiTools {
		var tool struct {
			Function struct {
				Name        string          `json:"name"`
				Description string          `json:"description,omitempty"`
				Parameters  json.RawMessage `json:"parameters,omitempty"`
			} `json:"function"`
		}
		if json.Unmarshal(t, &tool) != nil || tool.Function.Name == "" {
			continue
		}
		fd := map[string]interface{}{"name": tool.Function.Name, "description": tool.Function.Description}
		if len(tool.Function.Parameters) > 0 {
			var params map[string]interface{}
			if json.Unmarshal(tool.Function.Parameters, &params) == nil {
				fd["parameters"] = params
			}
		}
		result = append(result, map[string]interface{}{"functionDeclarations": []map[string]interface{}{fd}})
	}
	return result
}

// ---- Gemini→OpenAI request helpers ----

func extractGeminiSystemText(si json.RawMessage) string {
	var withParts struct {
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}
	if json.Unmarshal(si, &withParts) == nil {
		var r string
		for _, p := range withParts.Parts {
			r += p.Text
		}
		return r
	}
	var withText struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(si, &withText) == nil {
		return withText.Text
	}
	return ""
}

func mapGeminiRole(role string) string {
	switch role {
	case "model":
		return "assistant"
	case "function":
		return "tool"
	default:
		return "user"
	}
}

func geminiToolsToOpenAI(geminiTools []json.RawMessage) []map[string]interface{} {
	result := make([]map[string]interface{}, 0)
	for _, t := range geminiTools {
		var tool struct {
			FunctionDeclarations []json.RawMessage `json:"functionDeclarations"`
		}
		if json.Unmarshal(t, &tool) != nil {
			continue
		}
		for _, fd := range tool.FunctionDeclarations {
			var decl struct {
				Name        string          `json:"name"`
				Description string          `json:"description,omitempty"`
				Parameters  json.RawMessage `json:"parameters,omitempty"`
			}
			if json.Unmarshal(fd, &decl) != nil || decl.Name == "" {
				continue
			}
			ot := map[string]interface{}{
				"type":     "function",
				"function": map[string]interface{}{"name": decl.Name, "description": decl.Description},
			}
			if len(decl.Parameters) > 0 {
				ot["function"].(map[string]interface{})["parameters"] = decl.Parameters
			}
			result = append(result, ot)
		}
	}
	return result
}

// ---- Codex→OpenAI request helpers ----

func codexInputToMessages(input json.RawMessage) ([]json.RawMessage, error) {
	var str string
	if json.Unmarshal(input, &str) == nil {
		msg, _ := json.Marshal(map[string]interface{}{"role": "user", "content": str})
		return []json.RawMessage{msg}, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(input, &items); err != nil {
		return nil, fmt.Errorf("input must be string or array")
	}
	messages := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		var s string
		if json.Unmarshal(item, &s) == nil {
			msg, _ := json.Marshal(map[string]interface{}{"role": "user", "content": s})
			messages = append(messages, msg)
			continue
		}
		var msgObj struct {
			Role       string          `json:"role"`
			Content    json.RawMessage `json:"content"`
			ToolCallID string          `json:"tool_call_id,omitempty"`
		}
		if json.Unmarshal(item, &msgObj) != nil || msgObj.Role == "" {
			continue
		}
		openaiMsg := map[string]interface{}{"role": msgObj.Role, "content": msgObj.Content}
		if msgObj.ToolCallID != "" {
			openaiMsg["tool_call_id"] = msgObj.ToolCallID
		}
		var tcHolder struct {
			ToolCalls []json.RawMessage `json:"tool_calls,omitempty"`
		}
		if json.Unmarshal(item, &tcHolder) == nil && len(tcHolder.ToolCalls) > 0 {
			openaiMsg["tool_calls"] = tcHolder.ToolCalls
		}
		msgJSON, _ := json.Marshal(openaiMsg)
		messages = append(messages, msgJSON)
	}
	return messages, nil
}

// ---- Shared ----

func parseStopSequences(stop json.RawMessage) []string {
	var arr []string
	if json.Unmarshal(stop, &arr) == nil {
		return arr
	}
	var s string
	if json.Unmarshal(stop, &s) == nil {
		return []string{s}
	}
	return nil
}

func detectImageMIME(url string) string {
	lower := url
	if len(lower) > 100 {
		lower = lower[:100]
	}
	lower = lower + " " // pad to avoid index bounds
	switch {
	case contains(lower, ".png") || contains(lower, "image/png"):
		return "image/png"
	case contains(lower, ".jpg") || contains(lower, ".jpeg") || contains(lower, "image/jpeg") || contains(lower, "image/jpg"):
		return "image/jpeg"
	case contains(lower, ".webp") || contains(lower, "image/webp"):
		return "image/webp"
	case contains(lower, ".gif") || contains(lower, "image/gif"):
		return "image/gif"
	default:
		return "image/jpeg"
	}
}

func contains(s, substr string) bool {
	return len(substr) <= len(s) && containsStr(s, substr)
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
