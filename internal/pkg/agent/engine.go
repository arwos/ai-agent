/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.osspkg.com/logx"

	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/llm"
	"github.com/arwos/ai-agent/internal/pkg/prompts"
)

type ToolExecutor interface {
	Execute(context.Context, string, map[string]any) (string, error)
}

type Engine struct {
	Provider         llm.Provider
	ToolProvider     llm.Provider
	Tools            ToolExecutor
	Store            *dialog.Store
	MaxIterations    int
	SystemPrompt     string
	SystemContext    string
	ToolDefinitions  []llm.ToolDefinition
	Model            string
	ProviderName     string
	ToolModel        string
	ToolProviderName string
	Resume           bool
	// OnTool reports both the operation and the model that chose it. Tool calls
	// can be planned by either the main model or the dedicated tool model.
	OnTool func(model, provider, name string, arguments map[string]any, result string, err error)
	// OnReasoning receives only provider-explicit reasoning and is intentionally
	// not connected to dialog.Store.
	OnReasoning      func(text string)
	OnChunk          func(text string)
	streamedResponse bool
}

func (e *Engine) Run(ctx context.Context, id, prompt string) (result string, usage llm.Usage, err error) { //nolint:gocyclo // agent execution loop
	e.streamedResponse = false
	msgs := make([]llm.Message, 0, 8)
	// OpenAI-compatible chat templates (including Ollama's) require every
	// system message to precede the conversation. The provider prepends
	// SystemPrompt; keep the optional workspace AGENTS.md context immediately
	// after it, before persisted history.
	if strings.TrimSpace(e.SystemContext) != "" {
		msgs = append(msgs, llm.Message{Role: "system", Content: prompts.CompactText(e.SystemContext)})
	}
	if e.Store != nil {
		history, historyErr := e.Store.History(id, 0)
		if historyErr != nil {
			return "", usage, historyErr
		}
		start := 0
		for i, message := range history {
			if message.Compact {
				start = i
			}
		}
		for _, message := range history[start:] {
			if message.Error {
				continue
			}
			role := message.Role
			if role == "system" {
				role = "user"
			}
			if role == "tool_result" {
				// Persisted tool results must be available to a resumed run (for
				// example after a browser choice, reconnect, or goal verification).
				msgs = append(msgs, llm.Message{Role: "user", Content: prompts.ToolResult(message.Tool, message.Content)})
				continue
			}
			if role == "user" || role == "assistant" || role == "tool" {
				msgs = append(msgs, llm.Message{Role: role, Content: message.Content})
			}
		}
	}
	if !e.Resume {
		msgs = append(msgs, llm.Message{Role: "user", Content: prompt})
	}
	if e.Store != nil && !e.Resume {
		// Persist the user request before any tool activity so JSONL reflects
		// the actual execution order.
		if appendErr := e.Store.Append(id, dialog.Message{Role: "user", Content: prompt, Model: e.Model, Provider: e.ProviderName}); appendErr != nil {
			return "", usage, appendErr
		}
	}
	// The main model always sees the user's request first. A dedicated tool
	// model is only used after the main model explicitly decides that a tool is
	// needed, avoiding an unnecessary planning request for ordinary dialogue.
	r, err := e.chatCompletion(ctx, msgs)
	if err != nil {
		return "", usage, err
	}
	e.reportResponseReasoning(r)
	usage = addUsage(usage, r.Usage)
	if e.ToolProvider != nil && e.Tools != nil && len(e.ToolDefinitions) > 0 && len(responseToolCalls(r)) > 0 {
		// A main model can request more than one round of tools.  In particular,
		// models which express calls as textual JSON often ask for the next tool
		// only after seeing the preceding result.  Do not persist that JSON as a
		// final assistant reply: run another planning cycle until the main model
		// returns ordinary text.
		toolIterations := 0
		for len(responseToolCalls(r)) > 0 {
			remaining := 0
			if e.MaxIterations > 0 {
				remaining = e.MaxIterations - toolIterations
				if remaining <= 0 {
					return "", usage, fmt.Errorf("agent iteration limit exceeded")
				}
			}
			toolMessages, toolUsage, executed, toolErr := e.runToolCycle(ctx, id, msgs, remaining)
			usage = addUsage(usage, toolUsage)
			toolIterations += executed
			if toolErr != nil {
				return "", usage, toolErr
			}
			msgs = append(msgs, toolMessages...)
			r, err = e.mainChatCompletion(ctx, msgs, true, false)
			if err != nil {
				return "", usage, err
			}
			e.reportResponseReasoning(r)
			usage = addUsage(usage, r.Usage)
		}
		if strings.TrimSpace(r.Content) == "" {
			return "", usage, fmt.Errorf("provider returned an empty final response")
		}
		if e.Store != nil {
			_ = e.Store.Append(id, dialog.Message{Role: "assistant", Content: r.Content, Model: e.Model, Provider: e.ProviderName, ContextSize: usage.LastPromptTokens + usage.LastCompletionTokens, Tokens: usage.TotalTokens})
		}
		return r.Content, usage, nil
	}
	lastTool := ""
	iterations := 0
	for calls := responseToolCalls(r); len(calls) > 0 && e.Tools != nil; calls = responseToolCalls(r) {
		for _, call := range calls {
			if e.MaxIterations > 0 && iterations >= e.MaxIterations {
				return "", usage, fmt.Errorf("agent iteration limit exceeded")
			}
			iterations++
			v, x := e.executeTool(ctx, id, e.Model, e.ProviderName, call)
			lastTool = call.Name
			if x != nil {
				return "", usage, x
			}
			// Never copy malformed textual tool JSON back into the model context.
			msgs = append(msgs, llm.Message{Role: "assistant", Content: toolCallJSON(call)}, llm.Message{Role: "user", Content: prompts.ToolResult(call.Name, v)})
		}
		r, err = e.chatCompletion(ctx, msgs)
		if err != nil {
			return "", usage, err
		}
		e.reportResponseReasoning(r)
		usage = addUsage(usage, r.Usage)
	}
	if strings.TrimSpace(r.Content) == "" {
		if lastTool != "" {
			msgs = append(msgs, llm.Message{Role: "user", Content: prompts.ToolFinalRetry})
			// The tool sequence has already completed. Do not include native tool
			// schemas in this final turn: some providers otherwise return an empty
			// tool-planning response instead of explaining the collected results.
			r, err = e.mainChatCompletion(ctx, msgs, true, false)
			if err != nil {
				return "", usage, err
			}
			e.reportResponseReasoning(r)
			usage = addUsage(usage, r.Usage)
			if strings.TrimSpace(r.Content) != "" && len(responseToolCalls(r)) == 0 {
				// Continue below so the final response is persisted consistently.
			} else {
				return "", usage, fmt.Errorf("provider returned an empty final response after tool %q", lastTool)
			}
		}
		if strings.TrimSpace(r.Content) == "" {
			return "", usage, fmt.Errorf("provider returned an empty final response")
		}
	}
	if e.Store != nil {
		// The user entry was written before tool execution; update its context
		// metadata is intentionally left to the existing history model.
		_ = e.Store.Append(id, dialog.Message{Role: "assistant", Content: r.Content, Model: e.Model, Provider: e.ProviderName, ContextSize: usage.LastPromptTokens + usage.LastCompletionTokens, Tokens: usage.TotalTokens})
	}
	return r.Content, usage, nil
}

// runToolCycle delegates tool selection and tool-call iteration to ToolProvider.
// Its textual answer is intentionally not shown to the user: the main provider
// receives only the executed calls and their results and creates the response.
func (e *Engine) runToolCycle(ctx context.Context, id string, messages []llm.Message, maxIterations int) ([]llm.Message, llm.Usage, int, error) {
	var usage llm.Usage
	resultMessages := make([]llm.Message, 0, 4)
	r, err := e.toolChatCompletion(ctx, messages)
	if err != nil {
		return nil, usage, 0, err
	}
	usage = addUsage(usage, r.Usage)
	iterations := 0
	for calls := responseToolCalls(r); len(calls) > 0; calls = responseToolCalls(r) {
		for _, call := range calls {
			if maxIterations > 0 && iterations >= maxIterations {
				return nil, usage, iterations, fmt.Errorf("agent iteration limit exceeded")
			}
			iterations++
			value, callErr := e.executeTool(ctx, id, e.ToolModel, e.ToolProviderName, call)
			if callErr != nil {
				return nil, usage, iterations, callErr
			}
			resultMessages = append(resultMessages, llm.Message{Role: "assistant", Content: toolCallJSON(call)}, llm.Message{Role: "user", Content: prompts.ToolResult(call.Name, value)})
		}
		r, err = e.toolChatCompletion(ctx, append(append([]llm.Message(nil), messages...), resultMessages...))
		if err != nil {
			return nil, usage, iterations, err
		}
		usage = addUsage(usage, r.Usage)
	}
	return resultMessages, usage, iterations, nil
}

func responseToolCalls(response llm.Response) []llm.ToolCall {
	if len(response.ToolCalls) > 0 {
		return response.ToolCalls
	}
	if response.ToolCall != nil {
		return []llm.ToolCall{*response.ToolCall}
	}
	return nil
}

func (e *Engine) reportReasoning(response llm.Response) {
	if e.OnReasoning != nil && strings.TrimSpace(response.Reasoning) != "" {
		e.OnReasoning(response.Reasoning)
	}
}

func (e *Engine) reportResponseReasoning(response llm.Response) {
	if !e.streamedResponse {
		e.reportReasoning(response)
	}
}

func (e *Engine) executeTool(ctx context.Context, id, model, provider string, call llm.ToolCall) (string, error) {
	if e.OnTool != nil {
		e.OnTool(model, provider, call.Name, call.Arguments, "", nil)
	}
	if e.Store != nil {
		_ = e.Store.Append(id, dialog.Message{Role: "tool_call", Tool: call.Name, Arguments: call.Arguments, Model: model, Provider: provider})
	}
	value, err := e.Tools.Execute(ctx, call.Name, call.Arguments)
	if err != nil {
		logx.Error("agent tool execution failed", "tool", call.Name, "err", err)
		// A failed tool call is ordinary information for the model: a file may
		// be absent, a search may have no matches, or a request may be denied.
		// Feed it back into the ReAct loop so the model can explain the problem
		// or choose another tool instead of ending the whole dialogue.
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		encoded, marshalErr := json.Marshal(map[string]string{"error": err.Error()})
		if marshalErr != nil {
			return "", marshalErr
		}
		value = string(encoded)
	} else {
		logx.Info("agent tool executed", "tool", call.Name)
	}
	if e.OnTool != nil {
		e.OnTool(model, provider, call.Name, call.Arguments, value, err)
	}
	if e.Store != nil {
		_ = e.Store.Append(id, dialog.Message{Role: "tool_result", Tool: call.Name, Content: value, Model: model, Provider: provider})
	}
	return value, nil
}

// ExecuteTool replays a previously recorded evidence-gathering operation.
// Goal verification uses it after a user declines completion confirmation.
func (e *Engine) ExecuteTool(ctx context.Context, id string, call llm.ToolCall) (string, error) {
	return e.executeTool(ctx, id, e.Model, e.ProviderName, call)
}

func (e *Engine) chatCompletion(ctx context.Context, messages []llm.Message) (llm.Response, error) {
	return e.mainChatCompletion(ctx, messages, true, true)
}

func (e *Engine) mainChatCompletion(ctx context.Context, messages []llm.Message, stream, tools bool) (llm.Response, error) {
	systemPrompt := e.SystemPrompt
	if tools && len(e.ToolDefinitions) > 0 {
		if capable, ok := e.Provider.(llm.ToolCapabilityProvider); ok && !capable.SupportsNativeTools() {
			items := make([]struct {
				Name, Description string
				Parameters        map[string]any
			}, 0, len(e.ToolDefinitions))
			for _, tool := range e.ToolDefinitions {
				items = append(items, struct {
					Name, Description string
					Parameters        map[string]any
				}{tool.Name, tool.Description, tool.Parameters})
			}
			systemPrompt += prompts.TextToolInstructions(items)
		}
	}
	systemPrompt = prompts.CompactText(systemPrompt)
	call := func(provider llm.Provider) (llm.Response, error) {
		if streamProvider, ok := provider.(llm.StreamProvider); ok && stream {
			e.streamedResponse = true
			return streamProvider.ChatCompletionStream(ctx, systemPrompt, messages, tools, e.ToolDefinitions, func(delta llm.StreamDelta) error {
				if delta.Reasoning != "" {
					e.reportReasoning(llm.Response{Reasoning: delta.Reasoning})
				}
				if delta.Content != "" && e.OnChunk != nil {
					e.OnChunk(delta.Content)
				}
				return nil
			})
		}
		if withTools, ok := provider.(llm.ToolsProvider); ok && tools && len(e.ToolDefinitions) > 0 {
			return withTools.ChatCompletionWithTools(ctx, systemPrompt, messages, stream, e.ToolDefinitions)
		}
		return provider.ChatCompletion(ctx, systemPrompt, messages, stream)
	}
	r, err := call(e.Provider)
	if err != nil && isIncompleteToolCallError(err) {
		// Some local models emit the tool arguments followed by an incomplete
		// special token. Do not expose that protocol error to the user: give the
		// model a private correction turn and let it resend a structured call.
		retryMessages := append(append([]llm.Message(nil), messages...), llm.Message{
			Role:    "user",
			Content: "Your previous tool call was incomplete because it did not include a tool name. Retry now using a valid native tool call with the exact tool name and arguments. Do not answer with prose.",
		})
		r, err = callWithMessages(e.Provider, ctx, systemPrompt, retryMessages, stream, tools, e.ToolDefinitions, func(delta llm.StreamDelta) error {
			if delta.Reasoning != "" {
				e.reportReasoning(llm.Response{Reasoning: delta.Reasoning})
			}
			if delta.Content != "" && e.OnChunk != nil {
				e.OnChunk(delta.Content)
			}
			return nil
		})
		if err != nil && isIncompleteToolCallError(err) {
			retryMessages = append(retryMessages, llm.Message{Role: "user", Content: "The previous retry still omitted the tool name. Call one exact tool now; do not emit a JSON object without the tool field."})
			r, err = callWithMessages(e.Provider, ctx, systemPrompt, retryMessages, stream, tools, e.ToolDefinitions, func(delta llm.StreamDelta) error {
				if delta.Reasoning != "" {
					e.reportReasoning(llm.Response{Reasoning: delta.Reasoning})
				}
				if delta.Content != "" && e.OnChunk != nil {
					e.OnChunk(delta.Content)
				}
				return nil
			})
		}
	}
	if err != nil && tools && strings.Contains(strings.ToLower(err.Error()), "does not support tools") {
		// Some models reject the request before producing a response. Retry the
		// same model without native tool schemas; textual tool-call parsing may
		// still handle models that emit JSON in their answer.
		return e.Provider.ChatCompletion(ctx, e.SystemPrompt, messages, stream)
	}
	if err == nil && (r.ToolCall != nil || strings.TrimSpace(r.Content) != "") {
		return r, nil
	}
	if err == nil {
		// Let Run apply its normal empty-final-response and post-tool retry
		// handling. There is no automatic fallback provider anymore.
		return r, nil
	}
	return r, err
}

func isIncompleteToolCallError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "incomplete tool call")
}

func callWithMessages(provider llm.Provider, ctx context.Context, system string, messages []llm.Message, stream, tools bool, definitions []llm.ToolDefinition, emit func(llm.StreamDelta) error) (llm.Response, error) {
	if streamProvider, ok := provider.(llm.StreamProvider); ok && stream {
		return streamProvider.ChatCompletionStream(ctx, system, messages, tools, definitions, emit)
	}
	if withTools, ok := provider.(llm.ToolsProvider); ok && tools && len(definitions) > 0 {
		return withTools.ChatCompletionWithTools(ctx, system, messages, stream, definitions)
	}
	return provider.ChatCompletion(ctx, system, messages, stream)
}

func (e *Engine) toolChatCompletion(ctx context.Context, messages []llm.Message) (llm.Response, error) {
	if provider, ok := e.ToolProvider.(llm.ToolsProvider); ok {
		return provider.ChatCompletionWithTools(ctx, e.SystemPrompt, messages, false, e.ToolDefinitions)
	}
	return e.ToolProvider.ChatCompletion(ctx, e.SystemPrompt, messages, false)
}

func toolCallJSON(call llm.ToolCall) string {
	value, err := json.Marshal(map[string]any{"tool": call.Name, "arguments": call.Arguments})
	if err != nil {
		return `{"tool":"unknown","arguments":{}}`
	}
	return string(value)
}

func addUsage(total, current llm.Usage) llm.Usage {
	total.PromptTokens += current.PromptTokens
	total.CompletionTokens += current.CompletionTokens
	// Some compatible APIs omit total_tokens. In that case calculate this
	// request's cost from prompt/completion tokens, then add it even when a
	// preceding ReAct iteration did return total_tokens.
	requestTotal := current.TotalTokens
	if requestTotal == 0 {
		requestTotal = current.PromptTokens + current.CompletionTokens
	}
	total.TotalTokens += requestTotal
	total.LastPromptTokens = current.PromptTokens
	total.LastCompletionTokens = current.CompletionTokens
	return total
}

func (e *Engine) Validate() error {
	if e.Provider == nil {
		return fmt.Errorf("provider is required")
	}
	return nil
}
