/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"go.osspkg.com/goppy/v3/plugins/ws"
	"go.osspkg.com/goppy/v3/plugins/ws/event"

	"github.com/arwos/ai-agent/internal/pkg/mcp"
	"github.com/arwos/ai-agent/internal/pkg/models"
)

func (a *App) mcpList(ev event.Event, _ ws.Meta) error {
	if a.store != nil {
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		items, err := a.store.ProfileMCP(profileID)
		if err != nil {
			return err
		}
		builtins, err := a.builtinMCP(profileID)
		if err != nil {
			return err
		}
		items = append(items, builtins...)
		return ev.Encode(items)
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := append([]mcp.Config(nil), a.mcp...)
	sort.Slice(out, func(i, j int) bool { return out[i].Order < out[j].Order })
	return ev.Encode(out)
}
func (a *App) profileMCPUpsert(ev event.Event, _ ws.Meta) error {
	var request struct {
		ID    string          `json:"id"`
		Patch json.RawMessage `json:"patch"`
	}
	if err := ev.Decode(&request); err != nil {
		return err
	}
	var q models.MCPServer
	if request.ID != "" {
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		items, err := a.store.ProfileMCP(profileID)
		if err != nil {
			return err
		}
		builtins, err := a.builtinMCP(profileID)
		if err != nil {
			return err
		}
		items = append(items, builtins...)
		for _, item := range items {
			if item.ID == request.ID {
				q = item
				break
			}
		}
		if q.ID == "" {
			return fmt.Errorf("MCP server %q not found", request.ID)
		}
		if err = applyPatch(&q, request.Patch); err != nil {
			return err
		}
	} else if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.ProfileID == "" {
		id, err := a.activeProfile()
		if err != nil {
			return err
		}
		q.ProfileID = id
	}
	if q.ID == "" {
		q.ID = entityID("mcp")
	}
	if strings.HasPrefix(q.ID, builtinMCPIDPrefix) {
		item, err := a.updateBuiltinMCP(q.ProfileID, q)
		if err != nil {
			return err
		}
		return ev.Encode(item)
	}
	if err := a.validateMCPToolAliases(q); err != nil {
		return err
	}
	item, err := a.store.ProfileMCPUpsert(q)
	if err != nil {
		return err
	}
	return ev.Encode(item)
}
func (a *App) validateMCPToolAliases(candidate models.MCPServer) error {
	if a.store == nil {
		return nil
	}
	servers, err := a.store.ProfileMCP(candidate.ProfileID)
	if err != nil {
		return err
	}
	if builtins, builtinErr := a.builtinMCP(candidate.ProfileID); builtinErr != nil {
		return builtinErr
	} else {
		servers = append(servers, builtins...)
	}
	used := make(map[string]string)
	for _, server := range servers {
		if server.ID == candidate.ID {
			continue
		}
		for _, tool := range server.Tools {
			alias := strings.TrimSpace(tool.Alias)
			if alias == "" {
				alias = strings.TrimSpace(tool.Name)
			}
			if alias != "" {
				used[alias] = server.Name
			}
		}
	}
	conflicts := make([]string, 0)
	local := make(map[string]bool)
	for _, tool := range candidate.Tools {
		alias := strings.TrimSpace(tool.Alias)
		if alias == "" {
			alias = strings.TrimSpace(tool.Name)
		}
		if alias == "" {
			continue
		}
		if local[alias] {
			conflicts = append(conflicts, alias+" (duplicate in this server)")
			continue
		}
		local[alias] = true
		if server, exists := used[alias]; exists {
			conflicts = append(conflicts, alias+" (already used by "+server+")")
		}
	}
	if len(conflicts) > 0 {
		sort.Strings(conflicts)
		return fmt.Errorf("MCP tool aliases must be unique within the profile: %s", strings.Join(conflicts, ", "))
	}
	return nil
}
func (a *App) mcpSet(ev event.Event, _ ws.Meta) error {
	var q models.MCPConfig
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if q.Name == "" || q.Type == "" || q.Endpoint == "" {
		return fmt.Errorf("name, type and endpoint are required")
	}
	if q.Type != "http" && q.Type != "stdio" {
		return fmt.Errorf("unsupported mcp type")
	}
	if a.store == nil {
		return fmt.Errorf("configuration store unavailable")
	}
	item, err := a.store.MCPUpsert(q)
	if err != nil {
		return err
	}
	return ev.Encode(item)
}
func (a *App) mcpDelete(ev event.Event, _ ws.Meta) error {
	var q struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	if strings.HasPrefix(q.ID, builtinMCPIDPrefix) {
		return fmt.Errorf("system MCP server cannot be deleted")
	}
	if q.Name == "" && q.ID == "" {
		return fmt.Errorf("id or name is required")
	}
	if a.store == nil {
		return fmt.Errorf("configuration store unavailable")
	}
	if q.ID != "" {
		profileID, err := a.activeProfile()
		if err != nil {
			return err
		}
		if err = a.store.ProfileMCPDelete(profileID, q.ID); err != nil {
			return err
		}
		return ev.Encode(map[string]bool{"ok": true})
	}
	if err := a.store.MCPDelete(q.Name); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"ok": true})
}
func (a *App) mcpConfig(p json.RawMessage) (mcp.Config, error) {
	var q struct {
		Name string `json:"name"`
	}
	if e := decode(p, &q); e != nil {
		return mcp.Config{}, e
	}
	if a.store != nil {
		if profileID, err := a.activeProfile(); err == nil {
			if items, err := a.store.ProfileMCP(profileID); err == nil {
				for _, item := range items {
					if item.ID == q.Name || item.Name == q.Name {
						endpoint := item.URL
						if item.Transport == "stdio" {
							endpoint = item.Command
						}
						return mcp.Config{Name: item.Name, Type: item.Transport, Endpoint: endpoint, Prefix: item.Prefix}, nil
					}
				}
			}
		}
		items, e := a.store.MCPList()
		if e != nil {
			return mcp.Config{}, e
		}
		for _, x := range items {
			if x.Name == q.Name {
				return mcp.Config{Name: x.Name, Type: x.Type, Endpoint: x.Endpoint, Prefix: x.Prefix, Order: x.Order}, nil
			}
		}
		return mcp.Config{}, fmt.Errorf("mcp server %q not found", q.Name)
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, c := range a.mcp {
		if c.Name == q.Name {
			return c, nil
		}
	}
	return mcp.Config{}, fmt.Errorf("mcp server %q not found", q.Name)
}
func (a *App) mcpConfigFor(name string) (mcp.Config, error) {
	if a.store != nil {
		if profileID, err := a.activeProfile(); err == nil {
			if items, err := a.store.ProfileMCP(profileID); err == nil {
				for _, item := range items {
					if !item.Enabled || (item.ID != name && item.Name != name && item.Prefix != name) {
						continue
					}
					endpoint := item.URL
					if item.Transport == "stdio" {
						endpoint = item.Command
					}
					return mcp.Config{Name: item.Name, Type: item.Transport, Endpoint: endpoint, Prefix: item.Prefix}, nil
				}
			}
		}
		items, e := a.store.MCPList()
		if e != nil {
			return mcp.Config{}, e
		}
		for _, x := range items {
			if x.Name == name || x.Prefix == name {
				return mcp.Config{Name: x.Name, Type: x.Type, Endpoint: x.Endpoint, Prefix: x.Prefix, Order: x.Order}, nil
			}
		}
	}
	b, _ := json.Marshal(map[string]string{"name": name})
	return a.mcpConfig(b)
}
func (a *App) mcpHealth(ev event.Event, meta ws.Meta) error {
	var q struct {
		Name string `json:"name"`
	}
	if err := ev.Decode(&q); err != nil {
		return err
	}
	c, err := a.mcpConfig(mustJSON(q))
	if err != nil {
		return err
	}
	if err = a.mcpManager.Health(meta.Context(), c); err != nil {
		return err
	}
	return ev.Encode(map[string]bool{"healthy": true})
}
func (a *App) mcpTools(ev event.Event, meta ws.Meta) error {
	var q struct {
		Name      string `json:"name"`
		Transport string `json:"transport"`
		Command   string `json:"command"`
		URL       string `json:"url"`
		Prefix    string `json:"prefix"`
	}
	if e := ev.Decode(&q); e != nil {
		return e
	}
	var c mcp.Config
	var e error
	if q.Transport != "" {
		endpoint := q.URL
		if q.Transport == "stdio" {
			endpoint = q.Command
		}
		c = mcp.Config{Name: q.Name, Type: q.Transport, Endpoint: endpoint, Prefix: q.Prefix}
	} else {
		c, e = a.mcpConfig(mustJSON(q))
	}
	if e != nil {
		return e
	}
	info, e := a.mcpManager.Initialize(meta.Context(), c)
	if e != nil {
		return e
	}
	tools, e := a.mcpManager.ListTools(meta.Context(), c)
	if e != nil {
		return e
	}
	out := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]any{"name": tool.Name, "desc": tool.Description, "inputSchema": tool.InputSchema, "alias": tool.Name, "enabled": true})
	}
	if q.Name != "" && a.store != nil {
		if profileID, profileErr := a.activeProfile(); profileErr == nil {
			if servers, listErr := a.store.ProfileMCP(profileID); listErr == nil {
				for _, server := range servers {
					if server.Name != q.Name {
						continue
					}
					server.Instructions = info.Instructions
					server.Tools = make([]models.MCPTool, 0, len(tools))
					for _, tool := range tools {
						server.Tools = append(server.Tools, models.MCPTool{Name: tool.Name, Alias: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema, Enabled: true})
					}
					_, _ = a.store.ProfileMCPUpsert(server)
					break
				}
			}
		}
	}
	return ev.Encode(map[string]any{"tools": out})
}
