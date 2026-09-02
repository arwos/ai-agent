/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package memorytools

import (
	"context"

	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
)

func RegisterTools(tools *toolexecutor.Registry, store *dialog.MemoryStore) error {
	if err := tools.Register(toolexecutor.Tool{
		Name:        "memory.search",
		Description: "Search long-term notes and topic memories for the active profile. Use this when the user asks about remembered facts or an enabled skill requests memory lookup.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, "required": []string{"query"}},
		Handler: func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			query, _ := args["query"].(string)
			limit := 5
			if value, ok := args["limit"].(float64); ok && int(value) > 0 {
				limit = int(value)
			}
			return store.Relevant(scope.ProfileID, "", query, limit)
		},
	}); err != nil {
		return err
	}
	return tools.RegisterBuiltin(toolexecutor.BuiltinServer{Key: "memory", Name: "Memory", Description: "Search tools for long-term notes and topic memories.", Prefix: "memory", Tools: []toolexecutor.BuiltinTool{
		{ToolName: "memory.search", Alias: "search", Description: "Search long-term notes and topic memories when the user asks about remembered facts or a skill requests it.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, "required": []string{"query"}}},
	}})
}
