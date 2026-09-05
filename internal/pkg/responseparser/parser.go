// Package responseparser normalizes tool calls from common assistant APIs.
package responseparser

import (
	"encoding/json"
	"fmt"
	"strings"
)

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type OpenAIFragment struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

type openCall struct {
	ID, Name, Arguments string
	seen                map[string]struct{}
}

type OpenAIStream struct {
	calls map[int]*openCall
}

func (p *OpenAIStream) Add(fragment OpenAIFragment) {
	if p.calls == nil {
		p.calls = map[int]*openCall{}
	}
	call := p.calls[fragment.Index]
	if call == nil {
		call = &openCall{seen: map[string]struct{}{}}
		p.calls[fragment.Index] = call
	}
	if fragment.ID != "" {
		if call.ID == "" {
			call.ID = fragment.ID
		} else if call.ID != fragment.ID {
			call.ID = ""
		}
	}
	if fragment.Name != "" {
		call.Name += fragment.Name
	}
	if fragment.Arguments != "" {
		call.Arguments += fragment.Arguments
	}
}

func (p *OpenAIStream) Finish() ([]ToolCall, error) {
	if len(p.calls) == 0 {
		return nil, nil
	}
	indices := make([]int, 0, len(p.calls))
	for index := range p.calls {
		indices = append(indices, index)
	}
	for i := 1; i < len(indices); i++ {
		for j := i; j > 0 && indices[j] < indices[j-1]; j-- {
			indices[j], indices[j-1] = indices[j-1], indices[j]
		}
	}
	out := make([]ToolCall, 0, len(indices))
	for _, index := range indices {
		call := p.calls[index]
		if call.Name == "" || call.ID == "" {
			return nil, fmt.Errorf("malformed streamed tool call at index %d", index)
		}
		arguments, err := Arguments([]byte(call.Arguments))
		if err != nil {
			return nil, fmt.Errorf("invalid streamed tool arguments for %q: %w", call.Name, err)
		}
		out = append(out, ToolCall{ID: call.ID, Name: call.Name, Arguments: arguments})
	}
	return out, nil
}

func Arguments(raw json.RawMessage) (map[string]any, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return map[string]any{}, nil
	}
	if strings.HasPrefix(value, "```json") && strings.HasSuffix(value, "```") {
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "```json"), "```"))
	}
	arguments := map[string]any{}
	if json.Unmarshal([]byte(value), &arguments) == nil {
		return arguments, nil
	}
	var encoded string
	if err := json.Unmarshal([]byte(value), &encoded); err != nil {
		return nil, err
	}
	return argumentsFromString(encoded)
}

func argumentsFromString(value string) (map[string]any, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```json") && strings.HasSuffix(value, "```") {
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "```json"), "```"))
	}
	arguments := map[string]any{}
	if err := json.Unmarshal([]byte(value), &arguments); err != nil {
		return nil, err
	}
	return arguments, nil
}

type AnthropicBlock struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Text      string          `json:"text"`
}

type AnthropicStream struct {
	blocks map[int]*AnthropicBlock
}

func (p *AnthropicStream) Start(index int, block AnthropicBlock) {
	if p.blocks == nil {
		p.blocks = map[int]*AnthropicBlock{}
	}
	if block.Type == "tool_use" {
		p.blocks[index] = &block
	}
}

func (p *AnthropicStream) AddInput(index int, fragment string) error {
	block := p.blocks[index]
	if block == nil || block.Type != "tool_use" {
		return fmt.Errorf("unknown anthropic content block %d", index)
	}
	var current string
	if len(block.Input) > 0 {
		current = string(block.Input)
	}
	block.Input = json.RawMessage(current + fragment)
	return nil
}

func (p *AnthropicStream) Finish() ([]ToolCall, error) {
	blocks := make([]AnthropicBlock, 0, len(p.blocks))
	indices := make([]int, 0, len(p.blocks))
	for index := range p.blocks {
		indices = append(indices, index)
	}
	for i := 1; i < len(indices); i++ {
		for j := i; j > 0 && indices[j] < indices[j-1]; j-- {
			indices[j], indices[j-1] = indices[j-1], indices[j]
		}
	}
	for _, index := range indices {
		blocks = append(blocks, *p.blocks[index])
	}
	return AnthropicContent(blocks)
}

func AnthropicContent(content []AnthropicBlock) ([]ToolCall, error) {
	var out []ToolCall
	for _, block := range content {
		if block.Type != "tool_use" {
			continue
		}
		if block.ID == "" || block.Name == "" {
			return nil, fmt.Errorf("malformed anthropic tool_use block")
		}
		args, err := Arguments(block.Input)
		if err != nil {
			return nil, fmt.Errorf("invalid anthropic tool input for %q: %w", block.Name, err)
		}
		out = append(out, ToolCall{ID: block.ID, Name: block.Name, Arguments: args})
	}
	return out, nil
}

type GeminiPart struct {
	Text         string `json:"text"`
	Thought      bool   `json:"thought"`
	FunctionCall *struct {
		ID   string         `json:"id"`
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	} `json:"functionCall"`
}

func GeminiParts(parts []GeminiPart) ([]ToolCall, error) {
	var out []ToolCall
	for index, part := range parts {
		if part.FunctionCall == nil {
			continue
		}
		call := part.FunctionCall
		if call.Name == "" {
			return nil, fmt.Errorf("malformed gemini functionCall at index %d", index)
		}
		id := call.ID
		if id == "" {
			id = fmt.Sprintf("gemini-call-%d", index)
		}
		if call.Args == nil {
			call.Args = map[string]any{}
		}
		out = append(out, ToolCall{ID: id, Name: call.Name, Arguments: call.Args})
	}
	return out, nil
}

func PromptToolCalls(content string) []ToolCall {
	value := strings.ReplaceAll(strings.TrimSpace(content), `\_`, "_")
	if start := strings.Index(strings.ToLower(value), "```json"); start >= 0 {
		value = value[start+len("```json"):]
		if end := strings.Index(value, "```"); end >= 0 {
			value = value[:end]
		}
	}
	var out []ToolCall
	for offset := 0; offset < len(value); offset++ {
		if value[offset] != '{' {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(value[offset:]))
		var raw map[string]json.RawMessage
		if decoder.Decode(&raw) != nil {
			continue
		}
		var name string
		if json.Unmarshal(raw["tool"], &name) != nil || strings.TrimSpace(name) == "" {
			continue
		}
		args := map[string]any{}
		if value, ok := raw["arguments"]; ok && string(value) != "null" && json.Unmarshal(value, &args) != nil {
			continue
		}
		for key, value := range raw {
			if key == "tool" || key == "arguments" {
				continue
			}
			var decoded any
			if json.Unmarshal(value, &decoded) != nil {
				continue
			}
			args[key] = decoded
		}
		out = append(out, ToolCall{Name: name, Arguments: args})
	}
	return out
}
