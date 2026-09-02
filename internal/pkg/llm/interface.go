/*
 *  Copyright (c) 2026 Mikhail Knyazhev (markus621@yandex.com). All rights reserved.
 *  Use of this source code is governed by a GPL-3.0 license that can be found in the LICENSE file.
 */

package llm

import "context"

type ToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}
type Response struct {
	Content string
	// Reasoning is provider-supplied thinking text. It is transient UI data and
	// must never be persisted as conversation content.
	Reasoning string
	ToolCall  *ToolCall
	// ToolCalls preserves every operation returned in one provider response.
	// ToolCall remains for compatibility with single-call providers and tests.
	ToolCalls []ToolCall
	Usage     Usage
}
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	// LastPromptTokens and LastCompletionTokens describe the most recent
	// provider request. They are used for the actual context-window snapshot;
	// TotalTokens remains the cumulative consumption of the full agent run.
	LastPromptTokens     int
	LastCompletionTokens int
}
type Provider interface {
	ListModels(context.Context) ([]string, error)
	ChatCompletion(context.Context, string, []Message, bool) (Response, error)
}
type ToolDefinition struct {
	Name, Description string
	Parameters        map[string]any
}
type ToolsProvider interface {
	ChatCompletionWithTools(context.Context, string, []Message, bool, []ToolDefinition) (Response, error)
}
type StreamProvider interface {
	ChatCompletionStream(context.Context, string, []Message, bool, []ToolDefinition, func(StreamDelta) error) (Response, error)
}
type StreamDelta struct {
	Content, Reasoning string
	ToolCall           *ToolCall
}

// ToolCapabilityProvider exposes the cached native-tool capability.
type ToolCapabilityProvider interface{ SupportsNativeTools() bool }

// OpenAPIProvider is the single provider implementation. Endpoint paths are
// appended to BaseURL as-is; callers may include any provider-specific prefix
// (for example /v1) in BaseURL.
type OpenAPIProvider struct {
	Kind, BaseURL, APIKey, Model string
	Proxy                        *ProxyConfig
	RateLimitKey                 string
	RPM                          int
}

type Option func(*OpenAPIProvider)

// WithRPM limits requests made through this provider instance. A zero value
// disables throttling. The key must be stable for all instances of one saved
// provider so chat, models and metadata requests share the same budget.
func WithRPM(key string, rpm int) Option {
	return func(provider *OpenAPIProvider) {
		provider.RateLimitKey, provider.RPM = key, rpm
	}
}

func New(kind, baseURL, apiKey, model string, proxy *ProxyConfig, options ...Option) OpenAPIProvider {
	provider := OpenAPIProvider{Kind: kind, BaseURL: baseURL, APIKey: apiKey, Model: model, Proxy: proxy}
	for _, option := range options {
		option(&provider)
	}
	return provider
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type ProxyConfig struct {
	Type, Host, Username, Password string
	Port                           int
	InsecureSkipVerify             bool
}
type OpenAIProvider = OpenAPIProvider
type LlamaCppProvider = OpenAPIProvider
