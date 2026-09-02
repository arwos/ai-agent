/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package skills

import (
	"context"
	"fmt"

	"github.com/arwos/ai-agent/internal/pkg/models"
	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
)

// RegisterTools exposes skill instructions through the common tool
// registry. Skill content is loaded on demand, keeping the system prompt small
// while allowing the model to inspect the exact instruction file it needs.
func RegisterTools(tools *toolexecutor.Registry, service *Service) error {
	getTool := toolexecutor.Tool{
		Name:        "skills.get",
		Description: "Load the full instructions or an indexed related file for a selected skill. Use listFiles to discover companion files.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":      map[string]any{"type": "string", "description": "Skill name or ID."},
				"file":      map[string]any{"type": "string", "description": "Optional path relative to the skill folder, such as references/example.md."},
				"listFiles": map[string]any{"type": "boolean", "description": "Return the indexed files instead of file content."},
			},
			"required": []string{"name"},
		},
		Handler: func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			name, ok := args["name"].(string)
			if !ok || name == "" {
				return nil, fmt.Errorf("skill name is required")
			}
			if list, _ := args["listFiles"].(bool); list {
				files, err := service.Files(scope.ProfileID, name)
				if err != nil {
					return nil, err
				}
				return map[string]any{"name": name, "files": files}, nil
			}
			file, _ := args["file"].(string)
			content, err := service.ReadFile(scope.ProfileID, name, file)
			if err != nil {
				return nil, err
			}
			if file == "" {
				file = "SKILL.md"
			}
			return map[string]any{"name": name, "file": file, "content": content}, nil
		},
	}
	searchTool := toolexecutor.Tool{
		Name:        "skills.search",
		Description: "Search indexed skill names, descriptions, and instruction text. Use this to find a relevant skill or rule fragment before loading it.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"query":  map[string]any{"type": "string", "description": "Text to search for."},
			"cursor": map[string]any{"type": "string", "description": "Cursor returned by the previous page."},
			"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
		}, "required": []string{"query"}},
		Handler: func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			query, ok := args["query"].(string)
			if !ok || query == "" {
				return nil, fmt.Errorf("search query is required")
			}
			cursor, _ := args["cursor"].(string)
			limit := 20
			if value, ok := args["limit"].(float64); ok && int(value) > 0 {
				limit = int(value)
			}
			return service.SearchPage(scope.ProfileID, query, cursor, limit)
		},
	}
	createTool := toolexecutor.Tool{
		Name:        "skills.create",
		Description: "Create a profile skill. Set name to a unique human-readable skill name (letters, numbers, spaces, hyphens, or underscores; no paths), description to a short summary, and content to Markdown instructions. The profile and internal ID are assigned by the server.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"name":        map[string]any{"type": "string", "description": "Unique human-readable name, not a path. Example: Go Code Review."},
			"description": map[string]any{"type": "string"},
			"content":     map[string]any{"type": "string", "description": "Markdown skill instructions."},
		}, "required": []string{"name", "content"}},
		Handler: func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			name, ok := args["name"].(string)
			if !ok || name == "" {
				return nil, fmt.Errorf("skill name is required")
			}
			content, ok := args["content"].(string)
			if !ok {
				return nil, fmt.Errorf("skill content is required")
			}
			id := filesystemSkillID(name)
			description, _ := args["description"].(string)
			return service.Upsert(models.Skill{ID: id, ProfileID: scope.ProfileID, Name: name, Description: description, Content: content, Enabled: true})
		},
	}
	updateTool := toolexecutor.Tool{
		Name:        "skills.update",
		Description: "Update an existing profile skill selected by its name. Set description and/or content only when that field should change; at least one field is required. The internal ID is managed by the server.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"name":        map[string]any{"type": "string", "description": "Existing unique skill name, not a path."},
			"description": map[string]any{"type": "string", "description": "Optional replacement description."},
			"content":     map[string]any{"type": "string", "description": "Optional replacement Markdown instructions."},
		}, "required": []string{"name"}},
		Handler: func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			name, ok := args["name"].(string)
			if !ok || name == "" {
				return nil, fmt.Errorf("skill name is required")
			}
			description, hasDescription := args["description"].(string)
			content, hasContent := args["content"].(string)
			if !hasDescription && !hasContent {
				return nil, fmt.Errorf("description or content is required")
			}
			items, err := service.List(scope.ProfileID)
			if err != nil {
				return nil, err
			}
			var skill models.Skill
			for _, item := range items {
				if item.Name == name {
					skill = item
					break
				}
			}
			if skill.Name == "" {
				return nil, fmt.Errorf("skill %q not found", name)
			}
			if hasContent {
				skill.Content = content
			} else if skill.Content, err = service.Get(scope.ProfileID, skill.Name); err != nil {
				return nil, err
			}
			if hasDescription {
				skill.Description = description
			}
			return service.Upsert(skill)
		},
	}
	referenceTool := toolexecutor.Tool{
		Name:        "skills.set_reference",
		Description: "Save a code example or companion reference file inside an existing skill. The path is relative to the skill folder and must not be SKILL.md.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{
			"name":    map[string]any{"type": "string", "description": "Existing skill name."},
			"path":    map[string]any{"type": "string", "description": "Relative reference path, for example examples/sample.go; SKILL.md is forbidden."},
			"content": map[string]any{"type": "string", "description": "Reference or example content."},
		}, "required": []string{"name", "path", "content"}},
		Handler: func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			name, _ := args["name"].(string)
			file, _ := args["path"].(string)
			content, _ := args["content"].(string)
			if name == "" {
				return nil, fmt.Errorf("skill name is required")
			}
			if file == "" {
				return nil, fmt.Errorf("reference path is required")
			}
			if err := service.SetReference(scope.ProfileID, name, file, content); err != nil {
				return nil, err
			}
			return map[string]any{"name": name, "path": file, "saved": true}, nil
		},
	}
	for _, tool := range []toolexecutor.Tool{getTool, searchTool, createTool, updateTool, referenceTool} {
		if err := tools.Register(tool); err != nil {
			return err
		}
	}
	return tools.RegisterBuiltin(toolexecutor.BuiltinServer{
		Key: "skills", Name: "Skills", Description: "Load instructions and related files for skills selected for the active agent.", Prefix: "skills",
		Tools: []toolexecutor.BuiltinTool{{
			ToolName: "skills.get", Alias: "get", Description: "Load the full instructions or an indexed related file for a selected skill. Use listFiles to discover companion files.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{
				"name":      map[string]any{"type": "string", "description": "Skill name or ID."},
				"file":      map[string]any{"type": "string", "description": "Optional path relative to the skill folder."},
				"listFiles": map[string]any{"type": "boolean", "description": "Return indexed files instead of content."},
			}, "required": []string{"name"}},
		}, {
			ToolName: "skills.search", Alias: "search", Description: "Search indexed skill names, descriptions, and instruction text.", InputSchema: searchTool.InputSchema,
		}, {
			ToolName: "skills.create", Alias: "create", Description: "Create a profile skill. Use a unique human-readable name (letters, numbers, spaces, hyphens, or underscores; no paths), a short description, and Markdown instructions. Profile and internal ID are assigned by the server.", InputSchema: createTool.InputSchema,
		}, {
			ToolName: "skills.update", Alias: "update", Description: "Update an existing skill by name. Description and content are optional, but at least one must be supplied.", InputSchema: updateTool.InputSchema,
		}, {
			ToolName: "skills.set_reference", Alias: "set_reference", Description: "Save a code example or companion reference file inside a skill; the relative path must not be SKILL.md.", InputSchema: referenceTool.InputSchema,
		}},
	})
}
