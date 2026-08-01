// Package mutators implements the request mutator pipeline in upstream order:
// RTK → Headroom → Caveman → Ponytail → Pxpipe. Each mutator operates on
// req.MappedBody and preserves fail-open semantics: a failed mutator logs the
// error and hands the exact pre-mutator body to the next stage.
//
// Upstream authority: decolua/9router commit 79918c7830695bbca4a45c9fea4a42c3e9fd73d1
// Pipeline design: ARCHITECTURE.md §6, DECISIONS.md #108/#162 (fail-open)
package mutators

import (
	"encoding/json"
	"strings"
)

const systemSep = "\n\n"

func injectSystemPromptToBody(body map[string]json.RawMessage, prompt string) bool {
	if len(prompt) == 0 {
		return false
	}

	// OpenAI chat shape: messages[] with system role
	if msgsRaw, ok := body["messages"]; ok {
		var msgs []json.RawMessage
		if err := json.Unmarshal(msgsRaw, &msgs); err != nil {
			return false
		}
		msgs, changed := injectMessagesArray(msgs, prompt)
		if changed {
			raw, _ := json.Marshal(msgs)
			body["messages"] = raw
			return true
		}
		return false
	}

	// OpenAI Responses shape: input[] with system/developer role
	if inputRaw, ok := body["input"]; ok {
		var input []json.RawMessage
		if err := json.Unmarshal(inputRaw, &input); err != nil {
			return false
		}
		input, changed := injectMessagesArray(input, prompt)
		if changed {
			raw, _ := json.Marshal(input)
			body["input"] = raw
			return true
		}
	}

	// Try instructions (OpenAI Responses string)
	if instr, ok := body["instructions"]; ok {
		var instrStr string
		if err := json.Unmarshal(instr, &instrStr); err == nil {
			if instrStr == "" {
				body["instructions"], _ = json.Marshal(prompt)
			} else {
				body["instructions"], _ = json.Marshal(instrStr + systemSep + prompt)
			}
			return true
		}
	}

	return false
}

func injectMessagesArray(msgs []json.RawMessage, prompt string) ([]json.RawMessage, bool) {
	for i := range msgs {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(msgs[i], &msg); err != nil {
			continue
		}
		roleBytes, ok := msg["role"]
		if !ok {
			continue
		}
		var role string
		if err := json.Unmarshal(roleBytes, &role); err != nil {
			continue
		}
		if role == "system" || role == "developer" {
			content := getMessageContent(msg)
			if content != "" {
				msg["content"], _ = json.Marshal(content + systemSep + prompt)
			} else {
				msg["content"], _ = json.Marshal(prompt)
			}
			msgs[i], _ = json.Marshal(msg)
			return msgs, true
		}
	}
	// No system message found; prepend one — allocate N+1 slice preserving every original message
	sysMsg := map[string]json.RawMessage{
		"role":    mustJSON("system"),
		"content": mustJSON(prompt),
	}
	sysRaw, _ := json.Marshal(sysMsg)
	newMsgs := make([]json.RawMessage, 0, len(msgs)+1)
	newMsgs = append(newMsgs, sysRaw)
	newMsgs = append(newMsgs, msgs...)
	return newMsgs, true
}

func getMessageContent(msg map[string]json.RawMessage) string {
	content, ok := msg["content"]
	if !ok {
		return ""
	}
	var str string
	if err := json.Unmarshal(content, &str); err == nil {
		return str
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(content, &arr); err == nil {
		var parts []string
		for _, item := range arr {
			var itemMap map[string]json.RawMessage
			if err := json.Unmarshal(item, &itemMap); err != nil {
				continue
			}
			if text, ok := itemMap["text"]; ok {
				var t string
				if json.Unmarshal(text, &t) == nil {
					parts = append(parts, t)
				}
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

func mustJSON(v interface{}) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
