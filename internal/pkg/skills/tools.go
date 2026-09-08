/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package skills

import (
	"context"
	"crypto/sha256"
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
		Name:             "skills.create",
		Description:      "Create a profile skill. Set name to a unique human-readable skill name (letters, numbers, spaces, hyphens, or underscores; no paths), description to a short summary, and content to Markdown instructions. The profile and internal ID are assigned by the server.",
		RequiresApproval: true,
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
		Name:             "skills.update",
		Description:      "Update an existing profile skill selected by its name. Set description and/or content only when that field should change; at least one field is required. The internal ID is managed by the server.",
		RequiresApproval: true,
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
		Name:             "skills.set_reference",
		Description:      "Save a code example or companion reference file inside an existing skill. The path is relative to the skill folder and must not be SKILL.md.",
		RequiresApproval: true,
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
	groupList := toolexecutor.Tool{Name: "skills.groups.list", Description: "List skill groups in the active profile.", Handler: func(_ context.Context, scope toolexecutor.Scope, _ map[string]any) (any, error) {
		return service.Groups(scope.ProfileID)
	}}
	groupGet := toolexecutor.Tool{Name: "skills.groups.get", Description: "Get a skill group by ID.", Handler: func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
		id, _ := args["id"].(string)
		groups, err := service.Groups(scope.ProfileID)
		if err != nil {
			return nil, err
		}
		for _, group := range groups {
			if group.ID == id {
				return group, nil
			}
		}
		return nil, fmt.Errorf("skill group %q not found", id)
	}}
	groupCreate := groupWrite("create", service, func(_ []models.SkillGroup, args map[string]any) (models.SkillGroup, error) {
		name, _ := args["name"].(string)
		if name == "" {
			return models.SkillGroup{}, fmt.Errorf("group name is required")
		}
		sum := sha256.Sum256([]byte(name))
		return models.SkillGroup{ID: fmt.Sprintf("skill-group-%x", sum[:6]), ProfileID: "", Name: name}, nil
	})
	groupUpdate := groupWrite("update", service, func(groups []models.SkillGroup, args map[string]any) (models.SkillGroup, error) {
		id, _ := args["id"].(string)
		for _, group := range groups {
			if group.ID == id {
				if v, ok := args["name"].(string); ok {
					group.Name = v
				}
				if v, ok := args["description"].(string); ok {
					group.Description = v
				}
				if v, ok := args["apply_auto"].(bool); ok {
					group.ApplyAuto = v
				}
				return group, nil
			}
		}
		return models.SkillGroup{}, fmt.Errorf("skill group %q not found", id)
	})
	groupAssign := groupWrite("assign", service, func(groups []models.SkillGroup, args map[string]any) (models.SkillGroup, error) {
		id, _ := args["id"].(string)
		skillID, _ := args["skill_id"].(string)
		for _, group := range groups {
			if group.ID == id {
				skills, err := service.List(scopeProfile(args))
				found := false
				for _, skill := range skills {
					if skill.ID == skillID {
						found = true
						break
					}
				}
				if err != nil || !found {
					return models.SkillGroup{}, fmt.Errorf("skill %q not found", skillID)
				}
				group.SkillIDs = appendUnique(group.SkillIDs, skillID)
				return group, nil
			}
		}
		return models.SkillGroup{}, fmt.Errorf("skill group %q not found", id)
	})
	groupDelete := toolexecutor.Tool{Name: "skills.groups.delete", Description: "Delete an empty skill group in Design mode.", Handler: func(ctx context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
		if scope.Mode != "design" {
			return nil, fmt.Errorf("skill group changes are available only in design mode")
		}
		id, _ := args["id"].(string)
		groups, err := service.Groups(scope.ProfileID)
		if err != nil {
			return nil, err
		}
		for _, group := range groups {
			if group.ID == id {
				if len(group.SkillIDs) > 0 {
					return nil, fmt.Errorf("skill group %q still contains skills", id)
				}
				approved, approvalErr := skillApproval(scope, ctx, "delete skill group", group, nil)
				if approvalErr != nil || !approved {
					return nil, fmt.Errorf("skill group deletion was not approved")
				}
				next := make([]models.SkillGroup, 0, len(groups)-1)
				for _, candidate := range groups {
					if candidate.ID != id {
						next = append(next, candidate)
					}
				}
				if err := service.SaveGroups(scope.ProfileID, next); err != nil {
					return nil, err
				}
				return map[string]any{"deleted": true, "id": id}, nil
			}
		}
		return nil, fmt.Errorf("skill group %q not found", id)
	}}
	for _, tool := range []toolexecutor.Tool{getTool, searchTool, createTool, updateTool, referenceTool} {
		if err := tools.Register(tool); err != nil {
			return err
		}
	}
	for _, tool := range []toolexecutor.Tool{groupList, groupGet, groupCreate, groupUpdate, groupAssign, groupDelete} {
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
		}, {ToolName: "skills.groups.list", Alias: "groups.list", Description: groupList.Description}, {ToolName: "skills.groups.get", Alias: "groups.get", Description: groupGet.Description}, {ToolName: "skills.groups.create", Alias: "groups.create", Description: groupCreate.Description}, {ToolName: "skills.groups.update", Alias: "groups.update", Description: groupUpdate.Description}, {ToolName: "skills.groups.assign", Alias: "groups.assign", Description: groupAssign.Description}, {ToolName: "skills.groups.delete", Alias: "groups.delete", Description: groupDelete.Description}},
	})
}

func groupWrite(operation string, service *Service, build func([]models.SkillGroup, map[string]any) (models.SkillGroup, error)) toolexecutor.Tool {
	return toolexecutor.Tool{Name: "skills.groups." + operation, Description: "Create or update a skill group in Design mode.", Handler: func(ctx context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
		if scope.Mode != "design" {
			return nil, fmt.Errorf("skill group changes are available only in design mode")
		}
		groups, err := service.Groups(scope.ProfileID)
		if err != nil {
			return nil, err
		}
		args["_profile"] = scope.ProfileID
		group, err := build(groups, args)
		if err != nil {
			return nil, err
		}
		group.ProfileID = scope.ProfileID
		if operation == "create" {
			for _, existing := range groups {
				if existing.ID == group.ID || existing.Name == group.Name {
					return nil, fmt.Errorf("skill group already exists")
				}
			}
			groups = append(groups, group)
		} else {
			found := false
			for i := range groups {
				if groups[i].ID == group.ID {
					groups[i] = group
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("skill group %q not found", group.ID)
			}
		}
		if approved, approvalErr := skillApproval(scope, ctx, operation+" skill group", nil, group); approvalErr != nil || !approved {
			return nil, fmt.Errorf("skill group change was not approved")
		}
		if err := service.SaveGroups(scope.ProfileID, groups); err != nil {
			return nil, err
		}
		return map[string]any{"group": group, "operation": operation}, nil
	}}
}
func skillApproval(scope toolexecutor.Scope, ctx context.Context, detail string, before, after any) (bool, error) {
	if scope.Request == nil {
		return false, fmt.Errorf("approval is unavailable")
	}
	value, err := scope.Request(ctx, map[string]any{"kind": "approval", "title": "Skill configuration change requires approval", "detail": detail, "before": before, "after": after})
	if err != nil {
		return false, err
	}
	accepted, ok := value.(bool)
	return accepted && ok, nil
}
func appendUnique(values []string, value string) []string {
	for _, item := range values {
		if item == value {
			return values
		}
	}
	return append(values, value)
}
func scopeProfile(args map[string]any) string { value, _ := args["_profile"].(string); return value }
