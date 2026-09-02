/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package knowledge

import (
	"context"

	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
)

// RegisterTools exposes knowledge search as a profile-scoped system tool.
func RegisterTools(tools *toolexecutor.Registry, store *Store) error {
	if err := tools.Register(toolexecutor.Tool{
		Name:        "knowledge.search",
		Description: "Search the profile knowledge base. Use this when the user asks about stored documents or a skill explicitly requests knowledge-base search.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string", "description": "Text to search for."}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, "required": []string{"query"}},
		Handler: func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			query, _ := args["query"].(string)
			limit := store.SearchLimit()
			if value, ok := args["limit"].(float64); ok && int(value) > 0 {
				limit = int(value)
			}
			return store.Search(scope.ProfileID, query, limit)
		},
	}); err != nil {
		return err
	}
	return tools.RegisterBuiltin(toolexecutor.BuiltinServer{Key: "knowledge", Name: "Knowledge base", Description: "Search tools for the active profile knowledge base.", Prefix: "knowledge", Tools: []toolexecutor.BuiltinTool{
		{ToolName: "knowledge.search", Alias: "search", Description: "Search the profile knowledge base when the user asks about stored documents or a skill requests it.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string"}, "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20}}, "required": []string{"query"}}},
	}})
}

func NewToolRegistration(tools *toolexecutor.Registry, store *Store) (struct{}, error) {
	return struct{}{}, RegisterTools(tools, store)
}
