/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"fmt"
	"strings"

	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
)

const builtinMCPIDPrefix = "builtin:"

func (a *App) builtinMCP(profileID string) ([]models.MCPServer, error) {
	if a.tools == nil {
		return nil, nil
	}
	stored, err := a.store.BuiltinMCPSettings(profileID)
	if err != nil {
		return nil, err
	}
	settings := make(map[string]models.BuiltinMCPSettings, len(stored))
	for _, item := range stored {
		settings[item.BuiltinKey] = item
	}
	servers := make([]models.MCPServer, 0)
	for _, builtin := range a.tools.Builtins() {
		item, exists := settings[builtin.Key]
		if !exists {
			item = models.BuiltinMCPSettings{ProfileID: profileID, BuiltinKey: builtin.Key, Enabled: true}
		}
		configured := make(map[string]models.MCPTool, len(item.Tools))
		for _, tool := range item.Tools {
			configured[tool.Name] = tool
		}
		tools := make([]models.MCPTool, 0, len(builtin.Tools))
		for _, tool := range builtin.Tools {
			value, exists := configured[tool.ToolName]
			if !exists {
				value = models.MCPTool{Name: tool.ToolName, Alias: tool.Alias, Enabled: true}
			}
			// System MCP aliases are defined by the owning package and are not
			// profile settings. Ignore any legacy/custom value in storage.
			value.Alias = tool.Alias
			value.Name, value.Description, value.InputSchema = tool.ToolName, tool.Description, tool.InputSchema
			tools = append(tools, value)
		}
		servers = append(servers, models.MCPServer{ID: builtinMCPIDPrefix + builtin.Key, ProfileID: profileID, Name: builtin.Name, Transport: "builtin", Prefix: builtin.Prefix, Headers: []models.MCPHeader{}, Enabled: item.Enabled, Tools: tools, Instructions: builtin.Description, BuiltinKey: builtin.Key, System: true})
	}
	return servers, nil
}

func (a *App) updateBuiltinMCP(profileID string, request models.MCPServer) (models.MCPServer, error) {
	key := strings.TrimPrefix(request.ID, builtinMCPIDPrefix)
	if key == request.ID {
		return models.MCPServer{}, fmt.Errorf("invalid builtin MCP id")
	}
	servers, err := a.builtinMCP(profileID)
	if err != nil {
		return models.MCPServer{}, err
	}
	for _, server := range servers {
		if server.BuiltinKey != key {
			continue
		}
		// Only availability and tool enabled state are mutable for system MCPs.
		server.Enabled = request.Enabled
		configured := make(map[string]models.MCPTool, len(request.Tools))
		for _, tool := range request.Tools {
			configured[tool.Name] = tool
		}
		for i := range server.Tools {
			if value, exists := configured[server.Tools[i].Name]; exists {
				server.Tools[i].Enabled = value.Enabled
			}
		}
		if err = a.validateMCPToolAliases(server); err != nil {
			return models.MCPServer{}, err
		}
		if err = a.store.BuiltinMCPUpsert(models.BuiltinMCPSettings{ProfileID: profileID, BuiltinKey: key, Enabled: server.Enabled, Tools: server.Tools}); err != nil {
			return models.MCPServer{}, err
		}
		return server, nil
	}
	return models.MCPServer{}, fmt.Errorf("builtin MCP %q not found", key)
}

func builtinPromptTools(server models.MCPServer) ([]toolexecutor.Tool, map[string]string) {
	if !server.Enabled {
		return nil, nil
	}
	tools := make([]toolexecutor.Tool, 0, len(server.Tools))
	aliases := make(map[string]string)
	for _, tool := range server.Tools {
		if !tool.Enabled {
			continue
		}
		name := server.Prefix + "." + tool.Alias
		tools = append(tools, toolexecutor.Tool{Name: name, Description: tool.Description, InputSchema: tool.InputSchema})
		aliases[name] = tool.Name
	}
	return tools, aliases
}
