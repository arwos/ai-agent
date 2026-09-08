package workflow

import (
	"fmt"

	"github.com/arwos/ai-agent/internal/pkg/configstore"
	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
)

type ResourceCatalog struct {
	Servers []ResourceServer `json:"servers"`
	Agents  []AgentResources `json:"agents"`
}

type ResourceServer struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Prefix string         `json:"prefix"`
	Type   string         `json:"type"`
	Tools  []ResourceTool `json:"tools"`
}

type ResourceTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type AgentResources struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	MCPIDs []string `json:"mcpIds"`
	Tools  []string `json:"tools"`
}

func Catalog(profile string, registry *toolexecutor.Registry, store *configstore.Store) (ResourceCatalog, error) {
	settings, err := store.BuiltinMCPSettings(profile)
	if err != nil {
		return ResourceCatalog{}, err
	}
	configured := make(map[string]models.BuiltinMCPSettings, len(settings))
	for _, item := range settings {
		configured[item.BuiltinKey] = item
	}
	servers := make([]ResourceServer, 0)
	for _, builtin := range registry.Builtins() {
		setting, exists := configured[builtin.Key]
		if exists && !setting.Enabled {
			continue
		}
		overrides := make(map[string]bool, len(setting.Tools))
		for _, tool := range setting.Tools {
			overrides[tool.Name] = tool.Enabled
		}
		server := ResourceServer{ID: "builtin:" + builtin.Key, Name: builtin.Name, Prefix: builtin.Prefix, Type: "builtin"}
		for _, tool := range builtin.Tools {
			if enabled, set := overrides[tool.ToolName]; set && !enabled {
				continue
			}
			server.Tools = append(server.Tools, ResourceTool{Name: builtin.Prefix + "." + tool.Alias, Description: tool.Description})
		}
		if len(server.Tools) > 0 {
			servers = append(servers, server)
		}
	}
	external, err := store.ProfileMCP(profile)
	if err != nil {
		return ResourceCatalog{}, err
	}
	for _, item := range external {
		if !item.Enabled {
			continue
		}
		server := ResourceServer{ID: item.ID, Name: item.Name, Prefix: item.Prefix, Type: "external"}
		for _, tool := range item.Tools {
			if tool.Enabled {
				server.Tools = append(server.Tools, ResourceTool{Name: item.Prefix + "." + tool.Alias, Description: tool.Description})
			}
		}
		if len(server.Tools) > 0 {
			servers = append(servers, server)
		}
	}
	agents, err := store.Agents(profile)
	if err != nil {
		return ResourceCatalog{}, err
	}
	result := ResourceCatalog{Servers: servers, Agents: make([]AgentResources, 0, len(agents))}
	for _, agent := range agents {
		selected := make(map[string]bool, len(agent.MCPIDs)+1)
		for _, id := range agent.MCPIDs {
			selected[id] = true
		}
		available := AgentResources{ID: agent.ID, Name: agent.Name, MCPIDs: append([]string(nil), agent.MCPIDs...)}
		for _, server := range servers {
			if server.ID != "builtin:user" && !selected[server.ID] {
				continue
			}
			for _, tool := range server.Tools {
				available.Tools = append(available.Tools, tool.Name)
			}
		}
		result.Agents = append(result.Agents, available)
	}
	return result, nil
}

func ValidateResources(profile string, definition Definition, registry *toolexecutor.Registry, store *configstore.Store) error {
	catalog, err := Catalog(profile, registry, store)
	if err != nil {
		return err
	}
	tools := make(map[string]ResourceServer)
	for _, server := range catalog.Servers {
		for _, tool := range server.Tools {
			tools[tool.Name] = server
		}
	}
	agents := make(map[string]AgentResources, len(catalog.Agents))
	for _, agent := range catalog.Agents {
		agents[agent.ID] = agent
	}
	for _, step := range definition.Steps {
		agent, exists := agents[step.AgentID]
		if !exists {
			return fmt.Errorf("workflow step %q references unknown executor agent %q", step.ID, step.AgentID)
		}
		available := make(map[string]bool, len(agent.Tools))
		for _, tool := range agent.Tools {
			available[tool] = true
		}
		connected := make(map[string]bool, len(agent.MCPIDs)+1)
		for _, id := range agent.MCPIDs {
			connected[id] = true
		}
		connected["builtin:user"] = true
		for _, tool := range step.RequiredTools {
			server, exists := tools[tool]
			if !exists {
				return fmt.Errorf("workflow step %q requires unavailable tool %q", step.ID, tool)
			}
			if !available[tool] || !connected[server.ID] {
				return fmt.Errorf("workflow step %q requires tool %q from MCP %q, which is not connected to executor agent %q", step.ID, tool, server.Name, agent.Name)
			}
		}
	}
	return nil
}
