/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package gittools

import (
	"context"
	"fmt"
	"strings"

	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
	"github.com/arwos/ai-agent/internal/pkg/workspace"
)

func RegisterTools(tools *toolexecutor.Registry, workspaces *workspace.Registry) error {
	if err := tools.Register(toolexecutor.Tool{Name: "git.init", Description: "Initialize a Git repository in the active workspace.", RequiresApproval: true, InputSchema: objectSchema(), Handler: func(_ context.Context, scope toolexecutor.Scope, _ map[string]any) (any, error) {
		ws, err := workspaces.Get(scope.WorkspaceID)
		if err != nil {
			return nil, err
		}
		repo, err := Init(ws.Root())
		if err != nil {
			return nil, err
		}
		defer repo.Close()
		return map[string]bool{"ok": true}, nil
	}}); err != nil {
		return err
	}
	lookup := func(scope toolexecutor.Scope) (*Repository, error) {
		ws, err := workspaces.Get(scope.WorkspaceID)
		if err != nil {
			return nil, err
		}
		return Open(ws.Root())
	}
	register := func(name, description string, schema map[string]any, handler func(context.Context, *Repository, map[string]any) (any, error)) error {
		return tools.Register(toolexecutor.Tool{Name: name, Description: description, InputSchema: schema, Handler: func(ctx context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			if scope.Request != nil && strings.HasPrefix(name, "git.") && name != "git.status" && name != "git.diff" && name != "git.log" {
				approved, err := scope.Request(ctx, map[string]any{"kind": "approval", "title": "Git operation requires approval", "detail": description, "command": name, "arguments": args})
				if err != nil {
					return nil, err
				}
				if ok, yes := approved.(bool); !yes || !ok {
					return nil, fmt.Errorf("tool %q execution declined", name)
				}
			}
			repo, err := lookup(scope)
			if err != nil {
				return nil, err
			}
			defer repo.Close()
			return handler(ctx, repo, args)
		}})
	}
	if err := register("git.status", "Show staged, unstaged and untracked files in the active workspace Git repository.", objectSchema(), func(_ context.Context, r *Repository, _ map[string]any) (any, error) { return r.Status() }); err != nil {
		return err
	}
	if err := register("git.log", "Show recent commits in the active workspace Git repository.", map[string]any{"type": "object", "properties": map[string]any{"max_count": map[string]any{"type": "integer"}, "path": map[string]any{"type": "string"}}}, func(ctx context.Context, r *Repository, a map[string]any) (any, error) {
		max, _ := a["max_count"].(float64)
		p, _ := a["path"].(string)
		return r.Log(ctx, int(max), p)
	}); err != nil {
		return err
	}
	if err := register("git.diff", "Show a bounded diff summary for the active workspace Git repository.", map[string]any{"type": "object", "properties": map[string]any{"staged": map[string]any{"type": "boolean"}, "path": map[string]any{"type": "string"}}}, func(ctx context.Context, r *Repository, a map[string]any) (any, error) {
		staged, _ := a["staged"].(bool)
		p, _ := a["path"].(string)
		return r.Diff(ctx, staged, p)
	}); err != nil {
		return err
	}
	if err := register("git.add", "Stage files in the active workspace Git repository.", map[string]any{"type": "object", "properties": map[string]any{"files": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"files"}}, func(_ context.Context, r *Repository, a map[string]any) (any, error) {
		return nil, r.Add(stringsFromAny(a["files"]))
	}); err != nil {
		return err
	}
	if err := register("git.commit", "Create a commit with the fixed Agent AI author.", map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}, "required": []string{"message"}}, func(_ context.Context, r *Repository, a map[string]any) (any, error) {
		m, _ := a["message"].(string)
		h, err := r.Commit(m)
		return map[string]any{"hash": h}, err
	}); err != nil {
		return err
	}
	if err := register("git.checkout_branch", "Switch branches in the active workspace Git repository.", map[string]any{"type": "object", "properties": map[string]any{"branch_name": map[string]any{"type": "string"}, "create_if_missing": map[string]any{"type": "boolean"}}, "required": []string{"branch_name"}}, func(_ context.Context, r *Repository, a map[string]any) (any, error) {
		b, _ := a["branch_name"].(string)
		c, _ := a["create_if_missing"].(bool)
		return map[string]bool{"ok": true}, r.Checkout(b, c)
	}); err != nil {
		return err
	}
	if err := register("git.restore", "Restore staged files or require explicit confirmation before discarding working-tree changes.", map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "staged": map[string]any{"type": "boolean"}}, "required": []string{"path"}}, func(_ context.Context, r *Repository, a map[string]any) (any, error) {
		p, _ := a["path"].(string)
		staged, _ := a["staged"].(bool)
		if !staged {
			return nil, fmt.Errorf("discarding working-tree changes requires explicit confirmation")
		}
		return map[string]bool{"ok": true}, r.Restore([]string{p}, true)
	}); err != nil {
		return err
	}
	if err := register("git.push", "Push the current branch to a Git remote. Force push is never allowed.", map[string]any{"type": "object", "properties": map[string]any{"remote": map[string]any{"type": "string"}, "branch_name": map[string]any{"type": "string"}, "set_upstream": map[string]any{"type": "boolean"}, "force": map[string]any{"type": "boolean"}}}, func(ctx context.Context, r *Repository, a map[string]any) (any, error) {
		remote, _ := a["remote"].(string)
		branch, _ := a["branch_name"].(string)
		upstream, _ := a["set_upstream"].(bool)
		force, _ := a["force"].(bool)
		return map[string]bool{"ok": true}, r.Push(ctx, remote, branch, upstream, force)
	}); err != nil {
		return err
	}
	if err := register("git.create_pull_request", "Create a pull request through an explicitly configured HTTPS Git hosting API.", map[string]any{"type": "object", "properties": map[string]any{"api_url": map[string]any{"type": "string"}, "token": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "body": map[string]any{"type": "string"}, "head": map[string]any{"type": "string"}, "base": map[string]any{"type": "string"}}, "required": []string{"api_url", "title", "head", "base"}}, func(ctx context.Context, _ *Repository, a map[string]any) (any, error) {
		in := PullRequestInput{}
		in.APIURL, _ = a["api_url"].(string)
		in.Token, _ = a["token"].(string)
		in.Title, _ = a["title"].(string)
		in.Body, _ = a["body"].(string)
		in.Head, _ = a["head"].(string)
		in.Base, _ = a["base"].(string)
		return CreatePullRequest(ctx, in)
	}); err != nil {
		return err
	}
	return tools.RegisterBuiltin(toolexecutor.BuiltinServer{Key: "git", Name: "Git", Description: "Version-control tools for the active workspace.", Prefix: "git", Tools: []toolexecutor.BuiltinTool{
		{ToolName: "git.init", Alias: "init", Description: "Initialize a Git repository.", InputSchema: objectSchema()},
		{ToolName: "git.status", Alias: "status", Description: "Show repository status.", InputSchema: objectSchema()},
		{ToolName: "git.diff", Alias: "diff", Description: "Show repository diff.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"staged": map[string]any{"type": "boolean"}, "path": map[string]any{"type": "string"}}}},
		{ToolName: "git.log", Alias: "log", Description: "Show recent commits.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"max_count": map[string]any{"type": "integer"}, "path": map[string]any{"type": "string"}}}},
		{ToolName: "git.add", Alias: "add", Description: "Stage files.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"files": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}}, "required": []string{"files"}}},
		{ToolName: "git.commit", Alias: "commit", Description: "Create a commit.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"message": map[string]any{"type": "string"}}, "required": []string{"message"}}},
		{ToolName: "git.checkout_branch", Alias: "checkout_branch", Description: "Switch branches.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"branch_name": map[string]any{"type": "string"}, "create_if_missing": map[string]any{"type": "boolean"}}, "required": []string{"branch_name"}}},
		{ToolName: "git.restore", Alias: "restore", Description: "Restore staged files; working-tree discard requires confirmation.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"path": map[string]any{"type": "string"}, "staged": map[string]any{"type": "boolean"}}, "required": []string{"path"}}},
		{ToolName: "git.push", Alias: "push", Description: "Push to a remote; force push is prohibited.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"remote": map[string]any{"type": "string"}, "branch_name": map[string]any{"type": "string"}, "set_upstream": map[string]any{"type": "boolean"}, "force": map[string]any{"type": "boolean"}}}},
		{ToolName: "git.create_pull_request", Alias: "create_pull_request", Description: "Create a pull request through an HTTPS Git hosting API.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{"api_url": map[string]any{"type": "string"}, "token": map[string]any{"type": "string"}, "title": map[string]any{"type": "string"}, "body": map[string]any{"type": "string"}, "head": map[string]any{"type": "string"}, "base": map[string]any{"type": "string"}}, "required": []string{"api_url", "title", "head", "base"}}},
	}})
}

// Registration is a distinct DI result type so it cannot collide with other
// module registration markers in goppy's dependency graph.
type Registration struct{}

func NewToolRegistration(tools *toolexecutor.Registry, workspaces *workspace.Registry) (*Registration, error) {
	return &Registration{}, RegisterTools(tools, workspaces)
}

func objectSchema() map[string]any { return map[string]any{"type": "object"} }
func stringsFromAny(value any) []string {
	raw, _ := value.([]any)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
