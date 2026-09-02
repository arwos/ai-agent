package llm

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestOpenAPIProviderDoesNotDuplicateV1Prefix(t *testing.T) {
	p := OpenAPIProvider{BaseURL: "http://127.0.0.1:11434/v1"}
	if got := p.endpoint("/models"); got != "http://127.0.0.1:11434/v1/models" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestOllamaReasoningCapabilityCache(t *testing.T) {
	for _, tc := range []struct {
		model string
		want  bool
	}{
		{model: "aya:8b", want: true},
		{model: "qwen3:8b", want: true},
	} {
		if got := (OpenAPIProvider{Kind: "ollama", Model: tc.model}).supportsReasoning(); got != tc.want {
			t.Errorf("supportsReasoning(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
	if (OpenAPIProvider{Kind: "openai", Model: "qwen3:8b"}).supportsReasoning() {
		t.Fatal("non-Ollama providers must not receive Ollama reasoning options")
	}
}

func TestOllamaListModelsOpenAICompatibleResponse(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("listener unavailable: %v", err)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"llama3"},{"id":"qwen"}]}`))
	})
	server := &http.Server{Handler: handler}
	go func() { _ = server.Serve(listener) }()
	defer server.Close()
	models, err := (OpenAPIProvider{Kind: "ollama", BaseURL: "http://" + listener.Addr().String() + "/v1"}).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0] != "llama3" || models[1] != "qwen" {
		t.Fatalf("models = %#v", models)
	}
}

func TestOllamaThinkingUnsupportedIsCachedAndRetried(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	requests := 0
	http.DefaultClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		var body ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if requests == 1 {
			if body.ReasoningEffort != "medium" {
				t.Fatalf("first reasoning_effort = %q", body.ReasoningEffort)
			}
			return &http.Response{StatusCode: http.StatusBadRequest, Status: "400 Bad Request", Body: io.NopCloser(strings.NewReader(`{"error":{"message":"model does not support thinking"}}`)), Header: make(http.Header)}, nil
		}
		if body.ReasoningEffort != "" {
			t.Fatalf("retry reasoning_effort = %q", body.ReasoningEffort)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"ok"}}]}`)), Header: make(http.Header)}, nil
	})}
	p := OpenAPIProvider{Kind: "ollama", BaseURL: "http://mock", Model: "aya:8b-fallback-test"}
	got, err := p.ChatCompletion(context.Background(), "", []Message{{Role: "user", Content: "hi"}}, false)
	if err != nil || got.Content != "ok" || requests != 2 {
		t.Fatalf("first completion = %#v, err=%v, requests=%d", got, err, requests)
	}
	if _, err := p.ChatCompletion(context.Background(), "", []Message{{Role: "user", Content: "hi"}}, false); err != nil || requests != 3 {
		t.Fatalf("cached completion err=%v, requests=%d", err, requests)
	}
}

func TestHTTPProviderCompletion(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	http.DefaultClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		var request struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Messages) != 1 || request.Messages[0].Role != "user" || request.Messages[0].Content != "hi" {
			t.Fatalf("messages = %#v", request.Messages)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"hello"}}]}`)), Header: make(http.Header)}, nil
	})}
	p := OpenAPIProvider{BaseURL: "http://mock", Model: "test"}
	got, e := p.ChatCompletion(context.Background(), "", []Message{{Role: "user", Content: "hi"}}, false)
	if e != nil || got.Content != "hello" {
		t.Fatalf("%v %#v", e, got)
	}
}

func TestOllamaStreamingKeepsReasoningSeparateFromAnswer(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	http.DefaultClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		var request ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.ReasoningEffort != "medium" {
			t.Fatalf("reasoning_effort = %q", request.ReasoningEffort)
		}
		if request.StreamOptions == nil || !request.StreamOptions.IncludeUsage {
			t.Fatalf("stream_options = %#v", request.StreamOptions)
		}
		body := "data: {\"choices\":[{\"delta\":{\"content\":\"<th\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"ink>plan\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\" carefully</th\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"ink>Hello\"}}],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":5,\"total_tokens\":105}}\n\n" +
			"data: [DONE]\n\n"
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	var deltas []StreamDelta
	out, err := (OpenAPIProvider{Kind: "ollama", BaseURL: "http://mock", Model: "reasoning-model"}).ChatCompletionStream(context.Background(), "", []Message{{Role: "user", Content: "hi"}}, false, nil, func(delta StreamDelta) error {
		deltas = append(deltas, delta)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Content != "Hello" || out.Reasoning != "plan carefully" {
		t.Fatalf("response = %#v", out)
	}
	if out.Usage.PromptTokens != 100 || out.Usage.CompletionTokens != 5 || out.Usage.TotalTokens != 105 {
		t.Fatalf("usage = %#v", out.Usage)
	}
	var content, reasoning string
	for _, delta := range deltas {
		content += delta.Content
		reasoning += delta.Reasoning
	}
	if content != "Hello" || reasoning != "plan carefully" {
		t.Fatalf("deltas = %#v", deltas)
	}
}

func TestOpenAPIStreamingCollectsNativeToolCall(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	http.DefaultClient = &http.Client{Transport: roundTrip(func(_ *http.Request) (*http.Response, error) {
		body := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"fs.read_\",\"arguments\":\"{\\\"path\\\":\\\"AGE\"}}]}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"file\",\"arguments\":\"NTS.md\\\"}\"}}]}}]}\n\n" +
			"data: [DONE]\n\n"
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	out, err := (OpenAPIProvider{BaseURL: "http://mock", Model: "tool-model"}).ChatCompletionStream(context.Background(), "", []Message{{Role: "user", Content: "read file"}}, true, []ToolDefinition{{Name: "fs.read_file", Parameters: map[string]any{"type": "object"}}}, func(StreamDelta) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if out.ToolCall == nil || out.ToolCall.Name != "fs.read_file" || out.ToolCall.Arguments["path"] != "AGENTS.md" {
		t.Fatalf("tool call = %#v", out.ToolCall)
	}
}

func TestOpenAPIStreamingParsesTextualToolCall(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	http.DefaultClient = &http.Client{Transport: roundTrip(func(_ *http.Request) (*http.Response, error) {
		body := "data: {\"choices\":[{\"delta\":{\"content\":\"{\\\"arguments\\\":{\\\"path\\\":\\\"src\\\"},\\\"tool\\\":\\\"fs.list_\"}}]}\n\n" +
			"data: {\"choices\":[{\"delta\":{\"content\":\"dir\\\"}\"}}]}\n\n" +
			"data: [DONE]\n\n"
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	out, err := (OpenAPIProvider{BaseURL: "http://mock", Model: "tool-model"}).ChatCompletionStream(context.Background(), "", []Message{{Role: "user", Content: "list src"}}, false, nil, func(StreamDelta) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if out.ToolCall == nil || out.ToolCall.Name != "fs.list_dir" || out.ToolCall.Arguments["path"] != "src" {
		t.Fatalf("tool call = %#v", out.ToolCall)
	}
}

func TestOpenAPIProviderPlacesAllSystemMessagesFirst(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	http.DefaultClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		var request ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if len(request.Messages) != 3 || request.Messages[0].Role != "system" || request.Messages[0].Content != "main system\n\nworkspace instructions" || request.Messages[1].Role != "user" || request.Messages[2].Role != "assistant" {
			t.Fatalf("messages = %#v", request.Messages)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"hello"}}]}`)), Header: make(http.Header)}, nil
	})}
	p := OpenAPIProvider{BaseURL: "http://mock", Model: "test"}
	_, err := p.ChatCompletion(context.Background(), "main system", []Message{
		{Role: "user", Content: "old request"},
		{Role: "system", Content: "workspace instructions"},
		{Role: "assistant", Content: "old response"},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOrderSystemMessagesNormalizesSystemRole(t *testing.T) {
	messages := orderSystemMessages("", []Message{{Role: "user", Content: "request"}, {Role: "SYSTEM", Content: "instructions"}})
	if len(messages) != 2 || messages[0].Role != "system" || messages[1].Role != "user" {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestNormalizeAlternatingMessages(t *testing.T) {
	got := normalizeAlternatingMessages([]Message{
		{Role: "system", Content: "rules"},
		{Role: "user", Content: "first"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "answer"},
		{Role: "tool", Content: "result"},
		{Role: "user", Content: "follow-up"},
	})
	if len(got) != 4 || got[0].Role != "system" || got[1].Role != "user" || got[1].Content != "first\n\nsecond" || got[2].Role != "assistant" || got[3].Role != "user" || got[3].Content != "result\n\nfollow-up" {
		t.Fatalf("normalized messages = %#v", got)
	}
}

func TestOpenAPINativeToolCall(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	http.DefaultClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"","tool_calls":[{"id":"call-1","type":"function","function":{"name":"workspace.list","arguments":"{\"dir\":\".\"}"}}]}}]}`)), Header: make(http.Header)}, nil
	})}
	out, err := (OpenAPIProvider{BaseURL: "http://mock", Model: "test"}).ChatCompletionWithTools(context.Background(), "", nil, false, []ToolDefinition{{Name: "workspace.list", Parameters: map[string]any{"type": "object"}}})
	if err != nil || out.ToolCall == nil || out.ToolCall.Name != "workspace.list" || out.ToolCall.Arguments["dir"] != "." {
		t.Fatalf("out=%#v err=%v", out, err)
	}
}

func TestParseToolArguments(t *testing.T) {
	for _, raw := range []string{`{"path":"AGENTS.md"}`, `"{\"path\":\"AGENTS.md\"}"`, "```json\n{\"path\":\"AGENTS.md\"}\n```"} {
		arguments, err := parseToolArguments([]byte(raw))
		if err != nil || arguments["path"] != "AGENTS.md" {
			t.Fatalf("raw=%q arguments=%#v err=%v", raw, arguments, err)
		}
	}
	if _, err := parseToolArguments([]byte(`[]`)); err == nil {
		t.Fatal("expected invalid object error")
	}
}

func TestParsePromptToolCallWithAdjacentObjectsAndLegacyArguments(t *testing.T) {
	calls := parsePromptToolCalls(`{"arguments":{},"tool":"fs.list_dir","max_depth":2},{"arguments":{},"tool":"go.go_workspace"}`)
	if len(calls) != 2 {
		t.Fatalf("calls=%#v, want two tool calls", calls)
	}
	if calls[0].Name != "fs.list_dir" || calls[0].Arguments["max_depth"] != float64(2) || calls[1].Name != "go.go_workspace" {
		t.Fatalf("unexpected tool calls: %#v", calls)
	}
}

func TestParsePromptToolCallWithMarkdownEscapedToolName(t *testing.T) {
	calls := parsePromptToolCalls("I will inspect the source directory first.\n\n{\"arguments\":{\"path\":\"src\"},\"tool\":\"fs.list\\_dir\"}")
	if len(calls) != 1 || calls[0].Name != "fs.list_dir" || calls[0].Arguments["path"] != "src" {
		t.Fatalf("unexpected tool calls: %#v", calls)
	}
}

func TestParsePromptToolCallsSkipsBrokenObjectBeforeValidCall(t *testing.T) {
	content := "Okay, I will inspect the project.\n\n{\"arguments\":{\"max_depth\":3,\"path\":\".\"},}{\"arguments\":{\"path\":\"/workspace\"},\"tool\":\"fs.list_dir\"}"
	calls := parsePromptToolCalls(content)
	if len(calls) != 1 || calls[0].Name != "fs.list_dir" || calls[0].Arguments["path"] != "/workspace" {
		t.Fatalf("unexpected tool calls: %#v", calls)
	}
}

func TestParsePromptToolCallsWithSeveralAdjacentMarkdownEscapedCalls(t *testing.T) {
	content := "Project is TypeScript.\n\n{\"arguments\":{\"path\":\"package.json\"},\"tool\":\"fs.read\\_file\"}{\"arguments\":{\"path\":\"tsconfig.json\"},\"tool\":\"fs.read\\_file\"}{\"arguments\":{\"path\":\"vite.config.js\"},\"tool\":\"fs.read\\_file\"}{\"arguments\":{\"path\":\"pnpm-workspace.yaml\"},\"tool\":\"fs.read\\_file\"}{\"arguments\":{\"max\\_depth\":3,\"path\":\"src\"},\"tool\":\"fs.list\\_dir\"}"
	calls := parsePromptToolCalls(content)
	if len(calls) != 5 {
		t.Fatalf("calls=%#v, want five", calls)
	}
	if calls[0].Name != "fs.read_file" || calls[3].Arguments["path"] != "pnpm-workspace.yaml" || calls[4].Name != "fs.list_dir" || calls[4].Arguments["max_depth"] != float64(3) {
		t.Fatalf("unexpected calls: %#v", calls)
	}
}

func TestOllamaProvider(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	http.DefaultClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		body := `{"data":[{"id":"llama"}]}`
		if r.URL.Path == "/chat/completions" {
			body = `{"choices":[{"message":{"content":"hi"}}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	p := OpenAPIProvider{Kind: "ollama", BaseURL: "http://mock", Model: "llama"}
	models, e := p.ListModels(context.Background())
	if e != nil || len(models) != 1 {
		t.Fatalf("%v %#v", e, models)
	}
	out, e := p.ChatCompletion(context.Background(), "", nil, false)
	if e != nil || out.Content != "hi" {
		t.Fatalf("%v %#v", e, out)
	}
}

func TestContextWindowOpenAICompatibleUsesSelectedModelMetadata(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	http.DefaultClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer saved-token" {
			t.Fatalf("authorization = %q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"other","context_length":1024},{"id":"coder-mini","metadata":{"context_length":"32768"}}]}`))}, nil
	})}
	window, err := (OpenAPIProvider{Kind: "custom", BaseURL: "http://mock/v1", APIKey: "saved-token", Model: "coder-mini"}).ContextWindow(context.Background())
	if err != nil || window != 32768 {
		t.Fatalf("window=%d err=%v", window, err)
	}
}

func TestContextWindowOllamaUsesNativeEndpointAndAuthorization(t *testing.T) {
	old := http.DefaultClient
	defer func() { http.DefaultClient = old }()
	http.DefaultClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/v1/../api/show" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer ollama-token" {
			t.Fatalf("authorization = %q", got)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"model_info":{"general.context_length":8192}}`))}, nil
	})}
	window, err := (OpenAPIProvider{Kind: "Ollama", BaseURL: "http://mock/v1", APIKey: "ollama-token", Model: "qwen"}).ContextWindow(context.Background())
	if err != nil || window != 8192 {
		t.Fatalf("window=%d err=%v", window, err)
	}
}

func TestHTTPProviderUsesAuthenticatedHTTPProxy(t *testing.T) {
	var gotProxyAuth string
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Skipf("TCP listeners are unavailable: %v", err)
	}
	proxyServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotProxyAuth = r.Header.Get("Proxy-Authorization")
		if r.URL.String() == "" && r.RequestURI == "" {
			t.Fatalf("proxy did not receive a target URI")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"through proxy"}}]}`))
	}))
	proxyServer.Listener = listener
	proxyServer.Start()
	defer proxyServer.Close()

	p := OpenAPIProvider{
		BaseURL: "http://provider.invalid",
		Model:   "test",
		Proxy:   &ProxyConfig{Type: "http"},
	}
	// Keep the test independent from IPv4/IPv6 formatting by parsing the proxy URL.
	u, err := url.Parse(proxyServer.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	p.Proxy.Host, p.Proxy.Port, p.Proxy.Username, p.Proxy.Password = u.Hostname(), port, "proxy-user", "proxy-pass"

	got, err := p.ChatCompletion(context.Background(), "", []Message{{Role: "user", Content: "hello"}}, false)
	if err != nil || got.Content != "through proxy" {
		t.Fatalf("proxy request failed: %v %#v", err, got)
	}
	if gotProxyAuth != "Basic cHJveHktdXNlcjpwcm94eS1wYXNz" {
		t.Fatalf("unexpected proxy authorization: %q", gotProxyAuth)
	}
}
