/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package mcp

import (
	"context"

	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
)

// PromptTools exposes only enabled tools from MCP servers selected by an agent.
// Calls themselves are dispatched by the scoped MCP callback in toolexecutor.
func PromptTools(servers []models.MCPServer, selected []string) []toolexecutor.Tool {
	tools, _ := AgentTools(servers, selected)
	return tools
}

// AgentTools returns prompt metadata and the alias-to-real-name mapping for a
// single agent. Only tools explicitly enabled and attached to that agent exist
// in the map used by the scoped executor.
func AgentTools(servers []models.MCPServer, selected []string) ([]toolexecutor.Tool, map[string]toolexecutor.MCPTool) {
	allowed := make(map[string]bool, len(selected))
	for _, id := range selected {
		allowed[id] = true
	}
	result := make([]toolexecutor.Tool, 0)
	bindings := make(map[string]toolexecutor.MCPTool)
	for _, server := range servers {
		if !server.Enabled || !allowed[server.ID] {
			continue
		}
		for _, tool := range server.Tools {
			if !tool.Enabled {
				continue
			}
			alias := server.Prefix + "." + tool.Alias
			result = append(result, toolexecutor.Tool{
				Name:        alias,
				Description: tool.Description,
				InputSchema: tool.InputSchema,
				Handler:     func(context.Context, toolexecutor.Scope, map[string]any) (any, error) { return nil, nil },
			})
			bindings[alias] = toolexecutor.MCPTool{ServerName: server.Name, ToolName: tool.Name}
		}
	}
	return result, bindings
}
