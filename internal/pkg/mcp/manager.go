/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
)

type Config struct {
	Name, Type, Endpoint, Prefix string
	Order                        int
}
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

type InitializeInfo struct {
	Instructions string `json:"instructions"`
}
type Manager struct{ Servers []Config }

func New() *Manager { return &Manager{} }

// Initialize performs the MCP handshake and returns server-provided guidance.
// It is intentionally separate because the current manager creates a short-lived
// transport for each operation.
func (m Manager) Initialize(ctx context.Context, c Config) (InitializeInfo, error) {
	if c.Type == "stdio" {
		client, err := StartStdio(ctx, c.Endpoint)
		if err != nil {
			return InitializeInfo{}, err
		}
		defer client.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
		var info InitializeInfo
		if err = client.Call(ctx, "initialize", initializeParams, &info); err != nil {
			return info, err
		}
		return info, nil
	}
	var info InitializeInfo
	_, err := m.httpCall(ctx, c.Endpoint, "initialize", initializeParams, &info)
	return info, err
}

type CallResult struct {
	Content any `json:"content"`
}

var initializeParams = map[string]any{
	"protocolVersion": "2024-11-05",
	"capabilities":    map[string]any{},
	"clientInfo": map[string]any{
		"name":    "arwos-agent",
		"version": "1.0",
	},
}

func (m Manager) Health(ctx context.Context, c Config) error {
	if c.Type == "stdio" {
		cmd := exec.CommandContext(ctx, c.Endpoint, "--help") //nolint:gosec // validated input or bounded archive is required here
		return cmd.Run()
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint, nil)
	if e != nil {
		return e
	}
	res, e := http.DefaultClient.Do(r)
	if e != nil {
		return e
	}
	defer res.Body.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	if res.StatusCode >= 400 {
		return fmt.Errorf("mcp health: %s", res.Status)
	}
	return nil
}
func (m Manager) ListTools(ctx context.Context, c Config) ([]Tool, error) {
	if c.Type == "stdio" {
		client, e := StartStdio(ctx, c.Endpoint)
		if e != nil {
			return nil, e
		}
		defer client.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
		var initialized map[string]any
		if e = client.Call(ctx, "initialize", initializeParams, &initialized); e != nil {
			return nil, e
		}
		if e = client.Notify(ctx, "notifications/initialized", map[string]any{}); e != nil {
			return nil, e
		}
		var out struct {
			Tools []Tool `json:"tools"`
		}
		e = client.Call(ctx, "tools/list", map[string]any{}, &out)
		return out.Tools, e
	}
	sessionID, e := m.httpCall(ctx, c.Endpoint, "initialize", initializeParams, nil)
	if e != nil {
		return nil, e
	}
	if e = m.httpNotify(ctx, c.Endpoint, sessionID, "notifications/initialized", map[string]any{}); e != nil {
		return nil, e
	}
	b, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if e != nil {
		return nil, e
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	r.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		r.Header.Set("Mcp-Session-Id", sessionID)
	}
	res, e := http.DefaultClient.Do(r)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	var v struct {
		Result struct {
			Tools []Tool `json:"tools"`
		} `json:"result"`
	}
	e = json.NewDecoder(res.Body).Decode(&v)
	return v.Result.Tools, e
}
func (m Manager) CallTool(ctx context.Context, c Config, name string, args map[string]any) (any, error) {
	if c.Type == "stdio" {
		client, e := StartStdio(ctx, c.Endpoint)
		if e != nil {
			return nil, e
		}
		defer client.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
		var initialized map[string]any
		if e = client.Call(ctx, "initialize", initializeParams, &initialized); e != nil {
			return nil, e
		}
		if e = client.Notify(ctx, "notifications/initialized", map[string]any{}); e != nil {
			return nil, e
		}
		var out any
		e = client.Call(ctx, "tools/call", map[string]any{"name": name, "arguments": args}, &out)
		return out, e
	}
	sessionID, e := m.httpCall(ctx, c.Endpoint, "initialize", initializeParams, nil)
	if e != nil {
		return nil, e
	}
	if e = m.httpNotify(ctx, c.Endpoint, sessionID, "notifications/initialized", map[string]any{}); e != nil {
		return nil, e
	}
	b, e := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}})
	if e != nil {
		return nil, e
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(b))
	if e != nil {
		return nil, e
	}
	r.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		r.Header.Set("Mcp-Session-Id", sessionID)
	}
	res, e := http.DefaultClient.Do(r)
	if e != nil {
		return nil, e
	}
	defer res.Body.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	var v struct {
		Result any `json:"result"`
		Error  any `json:"error"`
	}
	if e = json.NewDecoder(res.Body).Decode(&v); e != nil {
		return nil, e
	}
	if v.Error != nil {
		return nil, fmt.Errorf("mcp tool error: %v", v.Error)
	}
	return v.Result, nil
}

func (m Manager) httpCall(ctx context.Context, endpoint, method string, params any, result any) (string, error) {
	b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return "", err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	r.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return "", err
	}
	defer res.Body.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	if res.StatusCode >= 400 {
		return "", fmt.Errorf("mcp: %s", res.Status)
	}
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err = json.NewDecoder(res.Body).Decode(&response); err != nil {
		return "", err
	}
	if response.Error != nil {
		return "", fmt.Errorf("mcp error: %s", response.Error.Message)
	}
	if result != nil {
		return res.Header.Get("Mcp-Session-Id"), json.Unmarshal(response.Result, result)
	}
	return res.Header.Get("Mcp-Session-Id"), nil
}

func (m Manager) httpNotify(ctx context.Context, endpoint, sessionID, method string, params any) error {
	b, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		return err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	if sessionID != "" {
		r.Header.Set("Mcp-Session-Id", sessionID)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return err
	}
	defer res.Body.Close() //nolint:errcheck // cleanup errors cannot be returned from this scope
	if res.StatusCode >= 400 {
		return fmt.Errorf("mcp: %s", res.Status)
	}
	return nil
}
