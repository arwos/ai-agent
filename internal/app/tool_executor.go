/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package app

import (
	"context"
	"fmt"

	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
)

type appToolExecutor struct {
	registry *toolexecutor.Registry
	scope    toolexecutor.Scope
}

func (e appToolExecutor) Execute(ctx context.Context, name string, args map[string]any) (string, error) {
	if e.registry == nil {
		return "", fmt.Errorf("tool registry is unavailable")
	}
	return e.registry.Execute(ctx, e.scope, name, args)
}

func (a *App) callMCPTool(ctx context.Context, tool toolexecutor.MCPTool, args map[string]any) (any, error) {
	if tool.ServerName == "" || tool.ToolName == "" {
		return nil, fmt.Errorf("invalid MCP tool registration")
	}
	c, x := a.mcpConfigFor(tool.ServerName)
	if x != nil {
		return nil, x
	}
	v, x := a.mcpManager.CallTool(ctx, c, tool.ToolName, args)
	if x != nil {
		return nil, x
	}
	return v, nil
}
