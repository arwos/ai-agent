/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package toolexecutor

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/arwos/ai-agent/internal/pkg/prompts"
)

type Tool struct {
	Name, Description string
	InputSchema       map[string]any
	RequiresApproval  bool
	Handler           func(context.Context, Scope, map[string]any) (any, error)
}

// BuiltinServer describes a system MCP provider. Its settings are stored per
// profile, while the executable handlers remain registered once through DI.
type BuiltinServer struct {
	Key, Name, Description, Prefix string
	Tools                          []BuiltinTool
}
type BuiltinTool struct {
	ToolName, Alias, Description string
	InputSchema                  map[string]any
}
type BuiltinSettings struct {
	Enabled bool                 `json:"enabled"`
	Tools   []BuiltinToolSetting `json:"tools"`
}
type BuiltinToolSetting struct {
	Name, Alias string `json:"name"`
	Enabled     bool   `json:"enabled"`
}

type Scope struct {
	WorkspaceID string
	ProfileID   string
	DialogID    string
	MCPTools    map[string]MCPTool
	Aliases     map[string]string
	Allowed     map[string]bool
	MCPCall     func(context.Context, MCPTool, map[string]any) (any, error)
	Request     func(context.Context, map[string]any) (any, error)
}

type MCPTool struct{ ServerName, ToolName string }

type Registry struct {
	mu       sync.RWMutex
	tools    map[string]Tool
	builtins map[string]BuiltinServer
}

func New() *Registry {
	return &Registry{tools: make(map[string]Tool), builtins: make(map[string]BuiltinServer)}
}

func (r *Registry) Register(tool Tool) error {
	if tool.Name == "" || tool.Description == "" || tool.Handler == nil {
		return fmt.Errorf("tool name, description and handler are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[tool.Name]; exists {
		return fmt.Errorf("tool %q is already registered", tool.Name)
	}
	r.tools[tool.Name] = tool
	return nil
}

// Select returns registered tools by name for composing a scoped model prompt.
// A copy is returned so callers cannot mutate the registry while a dialogue is
// running.
func (r *Registry) Select(names ...string) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tool, 0, len(names))
	for _, name := range names {
		if tool, ok := r.tools[name]; ok {
			out = append(out, tool)
		}
	}
	return out
}

// Description returns the human-readable purpose of a registered tool.
// It is used by progress/history views; execution continues to use the stable
// technical tool name.
func (r *Registry) Description(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if tool, ok := r.tools[name]; ok {
		return tool.Description
	}
	return ""
}

func (r *Registry) RegisterBuiltin(server BuiltinServer) error {
	if server.Key == "" || server.Name == "" || server.Prefix == "" {
		return fmt.Errorf("builtin MCP key, name and prefix are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.builtins[server.Key]; exists {
		return fmt.Errorf("builtin MCP %q is already registered", server.Key)
	}
	r.builtins[server.Key] = server
	return nil
}
func (r *Registry) Builtins() []BuiltinServer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]BuiltinServer, 0, len(r.builtins))
	for _, server := range r.builtins {
		out = append(out, server)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (r *Registry) Execute(ctx context.Context, scope Scope, name string, args map[string]any) (string, error) {
	aliased := false
	if target, exists := scope.Aliases[name]; exists {
		name = target
		aliased = true
	}
	r.mu.RLock()
	tool, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok && scope.MCPCall != nil {
		binding, allowed := scope.MCPTools[name]
		if !allowed {
			return "", fmt.Errorf("tool %q is not available for this agent", name)
		}
		value, err := scope.MCPCall(ctx, binding, args)
		if err != nil {
			return "", err
		}
		return marshal(value)
	}
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	if scope.Allowed != nil && (!scope.Allowed[name] || (scope.Aliases != nil && !aliased)) {
		return "", fmt.Errorf("tool %q is not available for this agent", name)
	}
	if tool.RequiresApproval {
		if scope.Request == nil {
			return "", fmt.Errorf("tool %q requires user approval", name)
		}
		answer, err := scope.Request(ctx, map[string]any{
			"kind": "approval", "title": "Tool execution requires approval",
			"detail": tool.Description, "command": name, "arguments": args,
		})
		if err != nil {
			return "", err
		}
		approved, ok := answer.(bool)
		if !ok || !approved {
			return "", fmt.Errorf("tool %q execution declined", name)
		}
	}
	value, err := tool.Handler(ctx, scope, args)
	if err != nil {
		return "", err
	}
	return marshal(value)
}

func (r *Registry) Prompt(extra []Tool) string {
	tools := append([]Tool(nil), extra...)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	if len(tools) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(prompts.ToolCallInstructions)
	b.WriteString(prompts.InteractiveToolInstruction)
	for _, tool := range tools {
		schema := tool.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object", "additionalProperties": true}
		}
		encoded, err := json.MarshalIndent(schema, "", "  ")
		if err != nil {
			encoded = []byte(`{"type":"object"}`)
		}
		b.WriteString(prompts.ToolSchema(tool.Name, tool.Description, string(encoded)))
	}
	return b.String()
}

func marshal(value any) (string, error) { data, err := json.Marshal(value); return string(data), err }
