/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package interaction

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
)

var (
	choiceIntent = regexp.MustCompile(`(?i)(?:\b(?:choose|select|pick|which option|what direction)\b|выбер(?:и|ите)|укаж(?:и|ите)|уточн(?:и|ите)|вариант|направлен)`)
	choiceLine   = regexp.MustCompile(`(?m)^\s*(?:\d+[.)]|[-*•])\s+(?:\*\*)?(.+?)(?:\*\*)?\s*$`)
)

// InferChoice is a deliberately narrow safety net for local models that
// ignore the user.choice instruction and print a numbered menu in prose. It
// never handles ordinary assistant lists: an explicit request-for-selection
// phrase and two to eight non-empty options are both required.
func InferChoice(content string) (map[string]any, bool) {
	content = strings.TrimSpace(content)
	if content == "" || !choiceIntent.MatchString(content) {
		return nil, false
	}
	matches := choiceLine.FindAllStringSubmatch(content, -1)
	if len(matches) < 2 || len(matches) > 8 {
		return nil, false
	}
	options := make([]map[string]string, 0, len(matches))
	for index, match := range matches {
		label := strings.TrimSpace(strings.Trim(match[1], "*- "))
		if label == "" {
			return nil, false
		}
		options = append(options, map[string]string{"id": fmt.Sprintf("option-%d", index+1), "label": label})
	}
	question := content
	if len(question) > 1200 {
		question = question[:1200]
	}
	return map[string]any{
		"kind": "choice", "title": "Choose how to continue", "question": question, "options": options,
	}, true
}

func RegisterTools(tools *toolexecutor.Registry) error {
	common := func(kind string, description string, schema map[string]any) error {
		return tools.Register(toolexecutor.Tool{Name: "user." + kind, Description: description, InputSchema: schema, Handler: func(ctx context.Context, scope toolexecutor.Scope, args map[string]any) (any, error) {
			if scope.Request == nil {
				return nil, fmt.Errorf("interactive requests are unavailable")
			}
			requestKind := kind
			if kind == "ask" {
				requestKind = "question"
			}
			args["kind"] = requestKind
			return scope.Request(ctx, args)
		}})
	}
	optionSchema := map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "string"}, "label": map[string]any{"type": "string"}, "hint": map[string]any{"type": "string"}}, "required": []string{"id", "label"}}
	requestSchema := map[string]any{"type": "object", "properties": map[string]any{"title": map[string]any{"type": "string"}, "detail": map[string]any{"type": "string"}, "question": map[string]any{"type": "string"}, "placeholder": map[string]any{"type": "string"}, "command": map[string]any{"type": "string"}, "options": map[string]any{"type": "array", "items": optionSchema}}, "required": []string{"title"}}
	askSchema := cloneSchema(requestSchema, []string{"title", "question"})
	choiceSchema := cloneSchema(requestSchema, []string{"title", "question", "options"})
	if err := common("ask", "Ask the user for free-form text input. Provide title and question.", askSchema); err != nil {
		return err
	}
	if err := common("choice", "Ask the user to choose exactly one option. Provide title, question, and two or more options with id and label.", choiceSchema); err != nil {
		return err
	}
	if err := common("multichoice", "Ask the user to choose one or more options. Provide title, question, and two or more options with id and label.", choiceSchema); err != nil {
		return err
	}
	if err := common("approval", "Ask the user to approve or decline an operation.", requestSchema); err != nil {
		return err
	}
	return tools.RegisterBuiltin(toolexecutor.BuiltinServer{Key: "user", Name: "User interaction", Description: "Tools for asking the user questions and confirmations.", Prefix: "user", Tools: []toolexecutor.BuiltinTool{
		{ToolName: "user.ask", Alias: "ask", Description: "Ask for free-form text.", InputSchema: requestSchema},
		{ToolName: "user.choice", Alias: "choice", Description: "Ask for one choice.", InputSchema: requestSchema},
		{ToolName: "user.multichoice", Alias: "multichoice", Description: "Ask for multiple choices.", InputSchema: requestSchema},
		{ToolName: "user.approval", Alias: "approval", Description: "Ask for approval.", InputSchema: requestSchema},
	}})
}

func cloneSchema(schema map[string]any, required []string) map[string]any {
	copy := make(map[string]any, len(schema))
	for key, value := range schema {
		copy[key] = value
	}
	copy["required"] = required
	return copy
}
