/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package llm

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"

	"go.osspkg.com/logx"
	"golang.org/x/net/proxy"

	"github.com/arwos/ai-agent/internal/pkg/utils"
)

type ChatRequest struct {
	Model           string         `json:"model"`
	Messages        []Message      `json:"messages"`
	Stream          bool           `json:"stream"`
	Tools           []apiTool      `json:"tools,omitempty"`
	ReasoningEffort string         `json:"reasoning_effort,omitempty"`
	StreamOptions   *streamOptions `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}
type apiTool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description,omitempty"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}
type chatResponse struct {
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			Reasoning string `json:"reasoning_content"`
			Thinking  string `json:"thinking"`
			Reason    string `json:"reasoning"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
}
type chatStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			Thinking         string `json:"thinking"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	PromptEvalCount int `json:"prompt_eval_count"`
	EvalCount       int `json:"eval_count"`
}

func (p OpenAPIProvider) ChatCompletionStream(ctx context.Context, system string, msgs []Message, withTools bool, tools []ToolDefinition, emit func(StreamDelta) error) (Response, error) {
	all := p.chatMessages(system, msgs)
	apiTools := make([]apiTool, 0, len(tools))
	if withTools && p.supportsTools() {
		for _, tool := range tools {
			item := apiTool{Type: "function"}
			item.Function.Name, item.Function.Description, item.Function.Parameters = tool.Name, tool.Description, tool.Parameters
			apiTools = append(apiTools, item)
		}
	}
	reqBody := ChatRequest{Model: p.Model, Messages: all, Stream: true, Tools: apiTools, StreamOptions: &streamOptions{IncludeUsage: true}}
	if p.supportsReasoning() {
		// Probe reasoning support once. Models that reject thinking are
		// remembered and retried without this optional parameter.
		reqBody.ReasoningEffort = "medium"
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return Response{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint("/chat/completions"), bytes.NewReader(b))
	if err != nil {
		return Response{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	if err := p.waitRateLimit(ctx); err != nil {
		return Response{}, p.providerError("chat/completions", "/chat/completions", "", err)
	}
	client, err := p.client()
	if err != nil {
		return Response{}, err
	}
	res, err := client.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		body, _ := utils.ReadAllResponse(res.Body)
		err := fmt.Errorf("provider returned %s: %s", res.Status, strings.TrimSpace(string(body)))
		if isAlternatingRolesError(err) {
			strictAlternating.Store(reasoningCapabilityKey(p), struct{}{})
			return p.ChatCompletionStream(ctx, system, msgs, withTools, tools, emit)
		}
		if reqBody.ReasoningEffort != "" && isUnsupportedThinkingError(err) {
			unsupportedReasoning.Store(reasoningCapabilityKey(p), struct{}{})
			return p.ChatCompletionStream(ctx, system, msgs, withTools, tools, emit)
		}
		return Response{}, err
	}
	var out Response
	var thinking thinkingStreamParser
	toolCalls := make(map[int]*streamToolCall)
	scanner := bufio.NewScanner(res.Body)
	scanner.Buffer(make([]byte, 4096), 100<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "[DONE]" {
			break
		}
		var chunk chatStreamChunk
		if json.Unmarshal([]byte(raw), &chunk) != nil {
			continue
		}
		if len(chunk.Choices) > 0 {
			d := chunk.Choices[0].Delta
			for _, deltaCall := range d.ToolCalls {
				call := toolCalls[deltaCall.Index]
				if call == nil {
					call = &streamToolCall{}
					toolCalls[deltaCall.Index] = call
				}
				if deltaCall.ID != "" {
					call.id = deltaCall.ID
				}
				call.name += deltaCall.Function.Name
				call.arguments += deltaCall.Function.Arguments
			}
			reasoning := d.ReasoningContent
			if reasoning == "" {
				reasoning = d.Reasoning
			}
			if reasoning == "" {
				reasoning = d.Thinking
			}
			content, embeddedReasoning := thinking.feed(d.Content)
			if reasoning == "" {
				reasoning = embeddedReasoning
			}
			out.Content += content
			out.Reasoning += reasoning
			if err := emit(StreamDelta{Content: content, Reasoning: reasoning}); err != nil {
				return out, err
			}
		}
		promptTokens, completionTokens, totalTokens := chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens, chunk.Usage.TotalTokens
		if promptTokens == 0 {
			promptTokens = chunk.PromptEvalCount
		}
		if completionTokens == 0 {
			completionTokens = chunk.EvalCount
		}
		if totalTokens == 0 && (promptTokens > 0 || completionTokens > 0) {
			totalTokens = promptTokens + completionTokens
		}
		if promptTokens > 0 || completionTokens > 0 || totalTokens > 0 {
			out.Usage.PromptTokens = promptTokens
			out.Usage.CompletionTokens = completionTokens
			out.Usage.TotalTokens = totalTokens
		}
	}
	content, reasoning := thinking.flush()
	out.Content += content
	out.Reasoning += reasoning
	if content != "" || reasoning != "" {
		if err := emit(StreamDelta{Content: content, Reasoning: reasoning}); err != nil {
			return out, err
		}
	}
	indices := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		call := toolCalls[index]
		arguments, parseErr := parseToolArguments(json.RawMessage(call.arguments))
		if parseErr != nil {
			return out, fmt.Errorf("invalid streamed tool arguments for %q: %w", call.name, parseErr)
		}
		out.ToolCalls = append(out.ToolCalls, ToolCall{Name: call.name, Arguments: arguments})
	}
	if len(out.ToolCalls) > 0 {
		out.ToolCall = &out.ToolCalls[0]
	} else if calls := parsePromptToolCalls(out.Content); len(calls) > 0 {
		// Some OpenAI-compatible models receive tools but still emit the
		// documented textual JSON form. Streaming has accumulated the complete
		// answer by this point, so parse it exactly as the non-streaming path.
		out.ToolCalls = calls
		out.ToolCall = &out.ToolCalls[0]
	}
	return out, scanner.Err()
}

type streamToolCall struct {
	id, name, arguments string
}

var unsupportedTools sync.Map
var unsupportedReasoning sync.Map
var strictAlternating sync.Map

func reasoningCapabilityKey(p OpenAPIProvider) string {
	return strings.ToLower(strings.TrimRight(p.BaseURL, "/")) + "\x00" + strings.ToLower(p.Model)
}

func isUnsupportedThinkingError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "does not support thinking")
}

func isAlternatingRolesError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "conversation roles must alternate") || strings.Contains(message, "roles must alternate")
}

func (p OpenAPIProvider) supportsReasoning() bool {
	if !strings.EqualFold(p.Kind, "ollama") {
		return false
	}
	_, disabled := unsupportedReasoning.Load(reasoningCapabilityKey(p))
	return !disabled
}

func (p OpenAPIProvider) chatMessages(system string, messages []Message) []Message {
	all := orderSystemMessages(system, messages)
	if _, enabled := strictAlternating.Load(reasoningCapabilityKey(p)); enabled {
		return normalizeAlternatingMessages(all)
	}
	return all
}

func toolsCapabilityKey(p OpenAPIProvider) string {
	return strings.ToLower(strings.TrimRight(p.BaseURL, "/")) + "\x00" + strings.ToLower(p.Model)
}
func (p OpenAPIProvider) supportsTools() bool {
	// This Ollama model family explicitly rejects the OpenAI tools field.
	// Keep the rule local to the provider capability check so it is not
	// duplicated in the agent orchestration layer.
	if strings.Contains(strings.ToLower(p.Model), "deepseek-coder-v2") {
		return false
	}
	_, disabled := unsupportedTools.Load(toolsCapabilityKey(p))
	return !disabled
}
func (p OpenAPIProvider) SupportsNativeTools() bool { return p.supportsTools() }

func (p OpenAPIProvider) providerError(operation, path, responseBody string, err error) error {
	if err != nil {
		logx.Error("provider API request failed", "operation", operation, "path", path, "response_body", responseBody, "provider_kind", p.Kind, "base_url", p.BaseURL, "model", p.Model, "err", err)
	}
	return err
}

func (p OpenAPIProvider) endpoint(path string) string {
	base := strings.TrimRight(p.BaseURL, "/")
	path = "/" + strings.TrimLeft(path, "/")
	return base + path
}
func (p OpenAPIProvider) client() (*http.Client, error) { return clientWithProxy(p.Proxy) }
func clientWithProxy(config *ProxyConfig) (*http.Client, error) {
	if config == nil || config.Host == "" {
		return http.DefaultClient, nil
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, fmt.Errorf("proxy port must be between 1 and 65535")
	}
	if config.Type != "http" && config.Type != "https" && config.Type != "socks5" && config.Type != "socks5h" {
		return nil, fmt.Errorf("unsupported proxy type %q", config.Type)
	}
	address := net.JoinHostPort(config.Host, strconv.Itoa(config.Port))
	if config.Type == "socks5" || config.Type == "socks5h" {
		var auth *proxy.Auth
		if config.Username != "" {
			auth = &proxy.Auth{User: config.Username, Password: config.Password}
		}
		dialer, err := proxy.SOCKS5("tcp", address, auth, proxy.Direct)
		if err != nil {
			return nil, err
		}
		return &http.Client{Transport: &http.Transport{DialContext: func(_ context.Context, network, addr string) (net.Conn, error) { return dialer.Dial(network, addr) }}}, nil
	}
	scheme := config.Type
	if scheme == "" {
		scheme = "http"
	}
	u, err := url.Parse(scheme + "://" + address)
	if err != nil {
		return nil, err
	}
	if config.Username != "" {
		u.User = url.UserPassword(config.Username, config.Password)
	}
	transport := &http.Transport{Proxy: http.ProxyURL(u)}
	if config.Type == "https" {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: config.InsecureSkipVerify}
	}
	return &http.Client{Transport: transport}, nil
}

// CheckProxyIP verifies a proxy by querying the public address endpoint.
// The request is intentionally made by the backend so proxy credentials never
// need to be exposed to the browser.
func CheckProxyIP(ctx context.Context, config *ProxyConfig) (string, error) {
	client, err := clientWithProxy(config)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ifconfig.me/ip", nil)
	if err != nil {
		return "", err
	}
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return "", fmt.Errorf("ip service returned %s", response.Status)
	}
	address, err := utils.ReadAllResponse(response.Body)
	if err != nil {
		return "", err
	}
	result := strings.TrimSpace(string(address))
	if result == "" {
		return "", fmt.Errorf("ip service returned an empty address")
	}
	return result, nil
}
func (p OpenAPIProvider) do(ctx context.Context, path string, in, out any) error {
	operation := strings.TrimPrefix(path, "/")
	b, e := json.Marshal(in)
	if e != nil {
		return p.providerError(operation, path, "", e)
	}
	r, e := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(path), bytes.NewReader(b))
	if e != nil {
		return p.providerError(operation, path, "", e)
	}
	r.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		r.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	if e := p.waitRateLimit(ctx); e != nil {
		return p.providerError(operation, path, "", e)
	}
	client, e := p.client()
	if e != nil {
		return p.providerError(operation, path, "", e)
	}
	res, e := client.Do(r)
	if e != nil {
		return p.providerError(operation, path, "", e)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		body, _ := utils.ReadAllResponse(res.Body)
		// Keep the provider's diagnostic in the returned error as well as in
		// logs. AgentEngine uses it to recognize capability errors such as
		// Ollama models that reject tool schemas.
		return p.providerError(operation, path, string(body), fmt.Errorf("provider returned %s: %s", res.Status, strings.TrimSpace(string(body))))
	}
	body, readErr := utils.ReadAllResponse(res.Body)
	if readErr != nil {
		return p.providerError(operation, path, string(body), readErr)
	}
	decodeErr := json.Unmarshal(body, out)
	return p.providerError(operation, path, string(body), decodeErr)
}
func (p OpenAPIProvider) ChatCompletion(ctx context.Context, system string, msgs []Message, stream bool) (Response, error) {
	return p.chatCompletion(ctx, system, msgs, stream, nil)
}
func (p OpenAPIProvider) ChatCompletionWithTools(ctx context.Context, system string, msgs []Message, stream bool, tools []ToolDefinition) (Response, error) {
	return p.chatCompletion(ctx, system, msgs, stream, tools)
}
func (p OpenAPIProvider) chatCompletion(ctx context.Context, system string, msgs []Message, stream bool, tools []ToolDefinition) (Response, error) {
	all := p.chatMessages(system, msgs)
	apiTools := make([]apiTool, 0, len(tools))
	if !p.supportsTools() {
		tools = nil
	}
	for _, tool := range tools {
		parameters := tool.Parameters
		if parameters == nil {
			parameters = map[string]any{"type": "object"}
		}
		item := apiTool{Type: "function"}
		item.Function.Name, item.Function.Description, item.Function.Parameters = tool.Name, tool.Description, parameters
		apiTools = append(apiTools, item)
	}
	var out chatResponse
	reasoningEffort := ""
	if p.supportsReasoning() {
		reasoningEffort = "medium"
	}
	e := p.do(ctx, "/chat/completions", ChatRequest{Model: p.Model, Messages: all, Stream: false, Tools: apiTools, ReasoningEffort: reasoningEffort}, &out)
	if e != nil {
		if isAlternatingRolesError(e) {
			strictAlternating.Store(reasoningCapabilityKey(p), struct{}{})
			return p.chatCompletion(ctx, system, msgs, stream, tools)
		}
		if reasoningEffort != "" && isUnsupportedThinkingError(e) {
			unsupportedReasoning.Store(reasoningCapabilityKey(p), struct{}{})
			return p.chatCompletion(ctx, system, msgs, stream, tools)
		}
		if len(apiTools) > 0 && strings.Contains(strings.ToLower(e.Error()), "does not support tools") {
			unsupportedTools.Store(toolsCapabilityKey(p), struct{}{})
			// Retry the same request without the unsupported native capability.
			return p.chatCompletion(ctx, system, msgs, stream, nil)
		}
		// Keep the payload private, but record its role sequence. It is enough to
		// diagnose strict model-template errors without leaking prompts or files.
		logx.Error("chat completion request rejected", "provider_kind", p.Kind, "model", p.Model, "message_roles", messageRoles(all), "err", e)
		return Response{}, e
	}
	if len(out.Choices) == 0 {
		return Response{}, fmt.Errorf("provider returned no choices")
	}
	c := out.Choices[0]
	reasoning := c.Message.Reasoning
	if reasoning == "" {
		reasoning = c.Message.Thinking
	}
	if reasoning == "" {
		reasoning = c.Message.Reason
	}
	content, embeddedReasoning := splitThinking(c.Message.Content)
	if reasoning == "" {
		reasoning = embeddedReasoning
	}
	r := Response{Content: content, Reasoning: reasoning, Usage: Usage{PromptTokens: out.Usage.PromptTokens, CompletionTokens: out.Usage.CompletionTokens, TotalTokens: out.Usage.TotalTokens}}
	if len(c.Message.ToolCalls) > 0 {
		for _, call := range c.Message.ToolCalls {
			a, parseErr := parseToolArguments(call.Function.Arguments)
			if parseErr != nil {
				err := fmt.Errorf("invalid tool arguments for %q: %w", call.Function.Name, parseErr)
				logx.Error("provider returned invalid tool arguments", "provider_kind", p.Kind, "base_url", p.BaseURL, "model", p.Model, "tool", call.Function.Name, "arguments", string(call.Function.Arguments), "err", err)
				return Response{}, err
			}
			r.ToolCalls = append(r.ToolCalls, ToolCall{Name: call.Function.Name, Arguments: a})
		}
	} else if calls := parsePromptToolCalls(c.Message.Content); len(calls) > 0 {
		r.ToolCalls = calls
	}
	if len(r.ToolCalls) > 0 {
		r.ToolCall = &r.ToolCalls[0]
	}
	return r, nil
}

// splitThinking handles Ollama-compatible models that embed their private
// reasoning in the visible content using <think>...</think> tags.
func splitThinking(content string) (string, string) {
	value := strings.ToLower(content)
	for _, pair := range [][2]string{{"<think>", "</think>"}, {"<|think|>", "</|think|>"}, {"<|thinking|>", "</|thinking|>"}} {
		start := strings.Index(value, pair[0])
		if start < 0 {
			continue
		}
		rest := content[start+len(pair[0]):]
		end := strings.Index(strings.ToLower(rest), pair[1])
		if end < 0 {
			return content, ""
		}
		return strings.TrimSpace(content[:start] + rest[end+len(pair[1]):]), strings.TrimSpace(rest[:end])
	}
	return content, ""
}

var thinkingMarkers = [][2]string{{"<think>", "</think>"}, {"<|think|>", "</|think|>"}, {"<|thinking|>", "</|thinking|>"}}

// thinkingStreamParser keeps tag boundaries between SSE chunks. A provider is
// free to split "<think>" and "</think>" across arbitrary events, so parsing
// each chunk separately would leak reasoning into the final answer or drop it.
type thinkingStreamParser struct {
	pending     string
	inReasoning bool
	closing     string
}

func (p *thinkingStreamParser) feed(chunk string) (content, reasoning string) {
	p.pending += chunk
	for p.pending != "" {
		if p.inReasoning {
			lower := strings.ToLower(p.pending)
			if end := strings.Index(lower, p.closing); end >= 0 {
				reasoning += p.pending[:end]
				p.pending = p.pending[end+len(p.closing):]
				p.inReasoning, p.closing = false, ""
				continue
			}
			safe, tail := splitPotentialMarker(p.pending, p.closing)
			reasoning += safe
			p.pending = tail
			break
		}

		lower := strings.ToLower(p.pending)
		start, marker := -1, [2]string{}
		for _, candidate := range thinkingMarkers {
			if index := strings.Index(lower, candidate[0]); index >= 0 && (start < 0 || index < start) {
				start, marker = index, candidate
			}
		}
		if start >= 0 {
			content += p.pending[:start]
			p.pending = p.pending[start+len(marker[0]):]
			p.inReasoning, p.closing = true, marker[1]
			continue
		}
		safe, tail := splitPotentialMarkers(p.pending, thinkingMarkers, 0)
		content += safe
		p.pending = tail
		break
	}
	return content, reasoning
}

func (p *thinkingStreamParser) flush() (content, reasoning string) {
	if p.inReasoning {
		return "", p.pending
	}
	return p.pending, ""
}

func splitPotentialMarkers(value string, markers [][2]string, markerIndex int) (string, string) {
	maxPrefix := 0
	lower := strings.ToLower(value)
	for _, marker := range markers {
		candidate := marker[markerIndex]
		for n := 1; n < len(candidate) && n <= len(value); n++ {
			if strings.HasSuffix(lower, candidate[:n]) && n > maxPrefix {
				maxPrefix = n
			}
		}
	}
	if maxPrefix == 0 {
		return value, ""
	}
	return value[:len(value)-maxPrefix], value[len(value)-maxPrefix:]
}

func splitPotentialMarker(value, marker string) (string, string) {
	return splitPotentialMarkers(value, [][2]string{{marker, marker}}, 0)
}

// orderSystemMessages makes requests compatible with strict chat templates
// (notably some Ollama model templates), which require exactly one system
// message at the beginning. The primary prompt and additional system contexts
// are merged in their original order without changing the following dialogue.
func orderSystemMessages(system string, messages []Message) []Message {
	systemParts := make([]string, 0, len(messages)+1)
	if strings.TrimSpace(system) != "" {
		systemParts = append(systemParts, system)
	}
	for _, message := range messages {
		if strings.EqualFold(message.Role, "system") && strings.TrimSpace(message.Content) != "" {
			systemParts = append(systemParts, message.Content)
		}
	}
	all := make([]Message, 0, len(messages)+1)
	if len(systemParts) > 0 {
		all = append(all, Message{Role: "system", Content: strings.Join(systemParts, "\n\n")})
	}
	for _, message := range messages {
		if !strings.EqualFold(message.Role, "system") {
			all = append(all, message)
		}
	}
	return all
}

// normalizeAlternatingMessages adapts a history to strict llama.app chat
// templates. Tool results are represented as user context by this provider
// implementation, and adjacent messages with the same role are coalesced.
func normalizeAlternatingMessages(messages []Message) []Message {
	result := make([]Message, 0, len(messages))
	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "tool" || role == "tool_result" {
			role = "user"
		}
		if role == "system" {
			if len(result) > 0 && result[0].Role == "system" {
				result[0].Content += "\n\n" + message.Content
			} else {
				result = append(result, Message{Role: "system", Content: message.Content})
			}
			continue
		}
		if role != "user" && role != "assistant" {
			continue
		}
		if len(result) > 0 && result[len(result)-1].Role == role {
			result[len(result)-1].Content += "\n\n" + message.Content
			continue
		}
		result = append(result, Message{Role: role, Content: message.Content})
	}
	return result
}

func messageRoles(messages []Message) []string {
	roles := make([]string, 0, len(messages))
	for _, message := range messages {
		roles = append(roles, message.Role)
	}
	return roles
}

func parseToolArguments(raw json.RawMessage) (map[string]any, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" {
		return map[string]any{}, nil
	}
	if strings.HasPrefix(value, "```json") && strings.HasSuffix(value, "```") {
		value = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(value, "```json"), "```"))
	}
	arguments := map[string]any{}
	if err := json.Unmarshal([]byte(value), &arguments); err == nil {
		return arguments, nil
	}
	var encoded string
	if err := json.Unmarshal([]byte(value), &encoded); err != nil {
		return nil, err
	}
	encoded = strings.TrimSpace(encoded)
	if strings.HasPrefix(encoded, "```json") && strings.HasSuffix(encoded, "```") {
		encoded = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(encoded, "```json"), "```"))
	}
	if err := json.Unmarshal([]byte(encoded), &arguments); err != nil {
		return nil, err
	}
	return arguments, nil
}

func parsePromptToolCalls(content string) []ToolCall {
	// Some local models apply Markdown escaping to tool names (for example,
	// fs.list\_dir). That is not a valid JSON escape sequence, but it is still
	// an unambiguous tool call and must not leak into the final assistant text.
	value := strings.ReplaceAll(strings.TrimSpace(content), `\_`, `_`)
	// Local models often add a short explanation before a JSON tool call. The
	// fenced block is still an unambiguous tool-call boundary, so extract it
	// before attempting to decode the response as JSON.
	if start := strings.Index(strings.ToLower(value), "```json"); start >= 0 {
		value = value[start+len("```json"):]
		if end := strings.Index(value, "```"); end >= 0 {
			value = value[:end]
		}
	}
	if call, ok := decodePromptToolCall([]byte(value)); ok {
		return []ToolCall{*call}
	}
	// Local models may wrap a call in prose or emit several JSON objects in
	// one answer. Try each object start; this deliberately avoids selecting the
	// nested `{}` belonging to an `arguments` field.
	calls := make([]ToolCall, 0, 2)
	for offset := 0; offset < len(value); offset++ {
		if value[offset] != '{' {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(value[offset:]))
		var raw map[string]json.RawMessage
		if err := decoder.Decode(&raw); err == nil {
			if call, ok := decodePromptToolMap(raw); ok {
				calls = append(calls, *call)
			}
		}
	}
	return calls
}

// decodePromptToolCall accepts the documented {tool, arguments} form and a
// common local-model variation where call parameters are placed beside
// "arguments". The latter must still execute as a tool call rather than leak
// its raw JSON into the visible assistant answer.
func decodePromptToolCall(data []byte) (*ToolCall, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, false
	}
	return decodePromptToolMap(raw)
}

func decodePromptToolMap(raw map[string]json.RawMessage) (*ToolCall, bool) {
	toolRaw, exists := raw["tool"]
	if !exists {
		return nil, false
	}
	var name string
	if err := json.Unmarshal(toolRaw, &name); err != nil || strings.TrimSpace(name) == "" {
		return nil, false
	}
	arguments := make(map[string]any)
	if value, exists := raw["arguments"]; exists && string(value) != "null" {
		if err := json.Unmarshal(value, &arguments); err != nil {
			return nil, false
		}
	}
	for key, value := range raw {
		if key == "tool" || key == "arguments" {
			continue
		}
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return nil, false
		}
		arguments[key] = decoded
	}
	return &ToolCall{Name: name, Arguments: arguments}, true
}
func (p OpenAPIProvider) ListModels(ctx context.Context) ([]string, error) {
	r, e := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint("/models"), nil)
	if e != nil {
		return nil, p.providerError("models", "/models", "", e)
	}
	if p.APIKey != "" {
		r.Header.Set("Authorization", "Bearer "+p.APIKey)
	}
	if e := p.waitRateLimit(ctx); e != nil {
		return nil, p.providerError("models", "/models", "", e)
	}
	client, e := p.client()
	if e != nil {
		return nil, p.providerError("models", "/models", "", e)
	}
	res, e := client.Do(r)
	if e != nil {
		return nil, p.providerError("models", "/models", "", e)
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		body, _ := utils.ReadAllResponse(res.Body)
		return nil, p.providerError("models", "/models", string(body), fmt.Errorf("provider returned %s", res.Status))
	}
	var raw struct {
		Data   json.RawMessage `json:"data"`
		Models json.RawMessage `json:"models"`
	}
	body, readErr := utils.ReadAllResponse(res.Body)
	if readErr != nil {
		return nil, p.providerError("models", "/models", string(body), readErr)
	}
	if e = json.Unmarshal(body, &raw); e != nil {
		return nil, p.providerError("models", "/models", string(body), e)
	}
	var data []struct {
		ID string `json:"id"`
	}
	if len(raw.Data) > 0 && json.Unmarshal(raw.Data, &data) == nil {
		out := make([]string, 0, len(data))
		for _, model := range data {
			if model.ID != "" {
				out = append(out, model.ID)
			}
		}
		return out, nil
	}
	var models []struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	if len(raw.Models) > 0 && json.Unmarshal(raw.Models, &models) == nil {
		out := make([]string, 0, len(models))
		for _, model := range models {
			if model.ID != "" {
				out = append(out, model.ID)
			} else if model.Name != "" {
				out = append(out, model.Name)
			}
		}
		return out, nil
	}
	return nil, p.providerError("models", "/models", string(body), fmt.Errorf("provider returned an unsupported models response"))
}
