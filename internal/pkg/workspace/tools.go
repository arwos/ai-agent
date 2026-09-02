/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package workspace

import (
	"context"
	"fmt"
	"strings"

	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
)

func RegisterTools(tools *toolexecutor.Registry, workspaces *Registry) error {
	lookup := func(scope toolexecutor.Scope) (*Service, error) { return workspaces.Get(scope.WorkspaceID) }
	for _, tool := range []toolexecutor.Tool{
		{Name: "fs.list_dir", Description: "Return files and directories in a workspace directory.", Handler: func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			w, err := lookup(scope)
			if err != nil {
				return nil, err
			}
			dir, _ := args["dir"].(string)
			if dir == "" {
				dir, _ = args["path"].(string)
			}
			return w.ListFiles(dir)
		}},
		{Name: "fs.read_file", Description: "Read a text file from the workspace with optional line range.", Handler: func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			w, err := lookup(scope)
			if err != nil {
				return nil, err
			}
			path, _ := args["path"].(string)
			if path == "" {
				return nil, fmt.Errorf("path is required")
			}
			content, err := w.ReadFile(path)
			if err != nil {
				return nil, err
			}
			offset, _ := args["offset"].(float64)
			limit, _ := args["limit"].(float64)
			if offset > 0 || limit > 0 {
				lines := strings.Split(content, "\n")
				start := int(offset)
				if start > len(lines) {
					start = len(lines)
				}
				end := len(lines)
				if limit > 0 && start+int(limit) < end {
					end = start + int(limit)
				}
				content = strings.Join(lines[start:end], "\n")
			}
			return content, nil
		}},
		{Name: "fs.write_file", Description: "Create or overwrite a workspace file.", Handler: func(_ context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			w, err := lookup(scope)
			if err != nil {
				return nil, err
			}
			path, _ := args["path"].(string)
			content, _ := args["content"].(string)
			if path == "" {
				return nil, fmt.Errorf("path is required")
			}
			return map[string]bool{"ok": true}, w.WriteFile(path, content)
		}},
	} {
		if err := tools.Register(tool); err != nil {
			return err
		}
	}
	advanced := []toolexecutor.Tool{
		{Name: "fs.edit_file", Description: "Make targeted edits to a file by replacing specified substrings.", InputSchema: editFileSchema(), Handler: func(_ context.Context, s toolexecutor.Scope, a map[string]any) (any, error) {
			w, e := lookup(s)
			if e != nil {
				return nil, e
			}
			p, _ := a["path"].(string)
			if p == "" {
				return nil, fmt.Errorf("path is required")
			}
			raw, ok := a["edits"].([]any)
			if !ok {
				return nil, fmt.Errorf("edits must be an array")
			}
			edits := make([]Edit, 0, len(raw))
			for _, item := range raw {
				value, ok := item.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("each edit must be an object")
				}
				oldText, oldOK := value["oldText"].(string)
				newText, newOK := value["newText"].(string)
				if !oldOK || !newOK {
					return nil, fmt.Errorf("each edit requires oldText and newText")
				}
				edits = append(edits, Edit{OldText: oldText, NewText: newText})
			}
			return map[string]bool{"ok": true}, w.EditFile(p, edits)
		}},
		{Name: "fs.mkdir", Description: "Create a directory in the workspace.", Handler: func(_ context.Context, s toolexecutor.Scope, a map[string]any) (any, error) {
			w, e := lookup(s)
			if e != nil {
				return nil, e
			}
			p, _ := a["path"].(string)
			return map[string]bool{"ok": true}, w.MakeDir(p)
		}},
		{Name: "fs.delete_file", Description: "Delete a file from the workspace.", RequiresApproval: true, Handler: func(_ context.Context, s toolexecutor.Scope, a map[string]any) (any, error) {
			w, e := lookup(s)
			if e != nil {
				return nil, e
			}
			p, _ := a["path"].(string)
			return map[string]bool{"ok": true}, w.RemoveFile(p)
		}},
		{Name: "fs.move_file", Description: "Move a file within the workspace.", RequiresApproval: true, Handler: func(_ context.Context, s toolexecutor.Scope, a map[string]any) (any, error) {
			w, e := lookup(s)
			if e != nil {
				return nil, e
			}
			src, _ := a["source"].(string)
			dst, _ := a["destination"].(string)
			return map[string]bool{"ok": true}, w.Move(src, dst)
		}},
		{Name: "fs.file_info", Description: "Return file metadata.", Handler: func(_ context.Context, s toolexecutor.Scope, a map[string]any) (any, error) {
			w, e := lookup(s)
			if e != nil {
				return nil, e
			}
			p, _ := a["path"].(string)
			return w.Info(p)
		}},
		{Name: "fs.search_replace", Description: "Replace one unique text fragment in a file.", Handler: func(_ context.Context, s toolexecutor.Scope, a map[string]any) (any, error) {
			w, e := lookup(s)
			if e != nil {
				return nil, e
			}
			p, _ := a["path"].(string)
			old, _ := a["old_text"].(string)
			n, _ := a["new_text"].(string)
			return map[string]bool{"ok": true}, w.Replace(p, old, n)
		}},
		{Name: "fs.grep", Description: "Search a regular expression in workspace files.", Handler: func(_ context.Context, s toolexecutor.Scope, a map[string]any) (any, error) {
			w, e := lookup(s)
			if e != nil {
				return nil, e
			}
			pattern, _ := a["pattern"].(string)
			dir, _ := a["path"].(string)
			include, _ := a["include"].(string)
			sensitive, _ := a["case_sensitive"].(bool)
			return w.Search(pattern, dir, include, sensitive)
		}},
	}
	for _, tool := range advanced {
		if err := tools.Register(tool); err != nil {
			return err
		}
	}
	return tools.RegisterBuiltin(toolexecutor.BuiltinServer{Key: "workspace", Name: "Filesystem", Description: "Filesystem tools for the active workspace.", Prefix: "fs", Tools: []toolexecutor.BuiltinTool{
		{ToolName: "fs.list_dir", Alias: "list_dir", Description: "Return files and directories in a workspace directory.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "recursive": map[string]any{"type": "boolean"}, "max_depth": map[string]any{"type": "integer"}}}},
		{ToolName: "fs.read_file", Alias: "read_file", Description: "Read a text file from the workspace. Supports optional zero-based line offset and line limit.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "offset": map[string]any{"type": "integer"}, "limit": map[string]any{"type": "integer"}}, "required": []string{"path"}}},
		{ToolName: "fs.write_file", Alias: "write_file", Description: "Create or overwrite a workspace file.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "content": map[string]any{"type": "string"}}, "required": []string{"path", "content"}}},
		{ToolName: "fs.mkdir", Alias: "mkdir", Description: "Create a workspace directory.", InputSchema: objectSchema("path")}, {ToolName: "fs.delete_file", Alias: "delete_file", Description: "Delete a workspace file.", InputSchema: objectSchema("path")}, {ToolName: "fs.move_file", Alias: "move_file", Description: "Move a workspace file.", InputSchema: objectSchema("source", "destination")}, {ToolName: "fs.file_info", Alias: "file_info", Description: "Return file metadata.", InputSchema: objectSchema("path")}, {ToolName: "fs.search_replace", Alias: "search_replace", Description: "Replace one unique text fragment.", InputSchema: objectSchema("path", "old_text", "new_text")},
		{ToolName: "fs.grep", Alias: "grep", Description: "Search a regex in workspace files.", InputSchema: objectSchema("pattern")},
		{ToolName: "fs.edit_file", Alias: "edit_file", Description: "Make targeted edits to a file by replacing specified substrings.", InputSchema: editFileSchema()},
	}})
}

func editFileSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type":        "string",
				"description": "Path to the file to edit",
			},
			"edits": map[string]any{
				"type":        "array",
				"description": "List of edit operations to apply sequentially",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"oldText": map[string]any{"type": "string", "description": "Exact text segment to replace"},
						"newText": map[string]any{"type": "string", "description": "Replacement text"},
					},
					"required": []string{"oldText", "newText"},
				},
			},
		},
		"required": []string{"path", "edits"},
	}
}

func objectSchema(names ...string) map[string]any {
	properties := map[string]any{}
	required := make([]string, 0, len(names))
	for _, name := range names {
		properties[name] = map[string]any{"type": "string"}
		required = append(required, name)
	}
	return map[string]any{"type": "object", "properties": properties, "required": required}
}

func NewToolRegistration(tools *toolexecutor.Registry, workspaces *Registry) (struct{}, error) {
	return struct{}{}, RegisterTools(tools, workspaces)
}
