package agent

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arwos/ai-agent/internal/pkg/dialog"
	"github.com/arwos/ai-agent/internal/pkg/llm"
)

type mockProvider struct {
	n        int
	requests [][]llm.Message
}

func (m *mockProvider) ListModels(context.Context) ([]string, error) { return []string{"mock"}, nil }
func (m *mockProvider) ChatCompletion(_ context.Context, _ string, messages []llm.Message, _ bool) (llm.Response, error) {
	m.requests = append(m.requests, append([]llm.Message(nil), messages...))
	m.n++
	if m.n == 1 {
		return llm.Response{ToolCall: &llm.ToolCall{Name: "echo", Arguments: map[string]any{"value": "x"}}}, nil
	}
	return llm.Response{Content: "done"}, nil
}

type emptyProvider struct{}

func (emptyProvider) ListModels(context.Context) ([]string, error) { return nil, nil }
func (emptyProvider) ChatCompletion(context.Context, string, []llm.Message, bool) (llm.Response, error) {
	return llm.Response{}, nil
}

type retryProvider struct{ n int }

func (*retryProvider) ListModels(context.Context) ([]string, error) { return []string{"mock"}, nil }
func (m *retryProvider) ChatCompletion(_ context.Context, _ string, _ []llm.Message, _ bool) (llm.Response, error) {
	m.n++
	switch m.n {
	case 1:
		return llm.Response{ToolCall: &llm.ToolCall{Name: "echo", Arguments: map[string]any{}}}, nil
	case 2:
		return llm.Response{}, nil
	default:
		return llm.Response{Content: "final after retry"}, nil
	}
}

type mockTools struct{}

func (mockTools) Execute(_ context.Context, n string, _ map[string]any) (string, error) {
	if n != "echo" {
		return "", context.Canceled
	}
	return "tool result", nil
}

type failingTools struct{}

func (failingTools) Execute(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "", fmt.Errorf("openat AGENTS.md: no such file or directory")
}

type toolFailureProvider struct {
	n        int
	requests [][]llm.Message
}

func (*toolFailureProvider) ListModels(context.Context) ([]string, error) {
	return []string{"mock"}, nil
}
func (p *toolFailureProvider) ChatCompletion(_ context.Context, _ string, messages []llm.Message, _ bool) (llm.Response, error) {
	p.n++
	p.requests = append(p.requests, append([]llm.Message(nil), messages...))
	if p.n == 1 {
		return llm.Response{ToolCall: &llm.ToolCall{Name: "fs.read_file", Arguments: map[string]any{"path": "AGENTS.md"}}}, nil
	}
	return llm.Response{Content: "AGENTS.md is not present in the workspace."}, nil
}

type streamingUsageProvider struct{}

func (streamingUsageProvider) ListModels(context.Context) ([]string, error) {
	return []string{"mock"}, nil
}
func (streamingUsageProvider) ChatCompletion(context.Context, string, []llm.Message, bool) (llm.Response, error) {
	return llm.Response{Content: "final", Usage: llm.Usage{PromptTokens: 40, CompletionTokens: 8, TotalTokens: 48}}, nil
}
func (streamingUsageProvider) ChatCompletionStream(_ context.Context, _ string, _ []llm.Message, _ bool, _ []llm.ToolDefinition, emit func(llm.StreamDelta) error) (llm.Response, error) {
	if err := emit(llm.StreamDelta{Content: "final"}); err != nil {
		return llm.Response{}, err
	}
	return llm.Response{Content: "final", Usage: llm.Usage{PromptTokens: 40, CompletionTokens: 8, TotalTokens: 48}}, nil
}

type multiToolProvider struct{ n int }

func (*multiToolProvider) ListModels(context.Context) ([]string, error) { return []string{"mock"}, nil }
func (m *multiToolProvider) ChatCompletion(_ context.Context, _ string, _ []llm.Message, _ bool) (llm.Response, error) {
	m.n++
	if m.n == 1 {
		return llm.Response{ToolCalls: []llm.ToolCall{{Name: "echo", Arguments: map[string]any{"value": "one"}}, {Name: "echo", Arguments: map[string]any{"value": "two"}}}}, nil
	}
	return llm.Response{Content: "done"}, nil
}

type finalProvider struct {
	requests [][]llm.Message
}

type mainToolRequestProvider struct {
	n        int
	requests [][]llm.Message
}

func (*mainToolRequestProvider) ListModels(context.Context) ([]string, error) {
	return []string{"main"}, nil
}
func (m *mainToolRequestProvider) ChatCompletion(_ context.Context, _ string, messages []llm.Message, _ bool) (llm.Response, error) {
	m.requests = append(m.requests, append([]llm.Message(nil), messages...))
	m.n++
	if m.n == 1 {
		return llm.Response{ToolCall: &llm.ToolCall{Name: "echo", Arguments: map[string]any{"value": "x"}}}, nil
	}
	return llm.Response{Content: "main model answer"}, nil
}

func (*finalProvider) ListModels(context.Context) ([]string, error) { return []string{"main"}, nil }
func (m *finalProvider) ChatCompletion(_ context.Context, _ string, messages []llm.Message, _ bool) (llm.Response, error) {
	m.requests = append(m.requests, append([]llm.Message(nil), messages...))
	return llm.Response{Content: "main model answer"}, nil
}

type toolPlannerProvider struct{ n int }

func (*toolPlannerProvider) ListModels(context.Context) ([]string, error) {
	return []string{"tool"}, nil
}

type failingProvider struct{}

func (failingProvider) ListModels(context.Context) ([]string, error) { return nil, nil }
func (failingProvider) ChatCompletion(context.Context, string, []llm.Message, bool) (llm.Response, error) {
	return llm.Response{}, context.DeadlineExceeded
}
func (m *toolPlannerProvider) ChatCompletion(_ context.Context, _ string, _ []llm.Message, _ bool) (llm.Response, error) {
	m.n++
	if m.n == 1 {
		return llm.Response{ToolCall: &llm.ToolCall{Name: "echo", Arguments: map[string]any{"value": "x"}}}, nil
	}
	return llm.Response{Content: "tool planning complete"}, nil
}

// repeatedMainToolProvider models an OpenAI-compatible response whose textual
// JSON was parsed into ToolCall. The main model asks for another operation
// after receiving the first result, then finally answers normally.
type repeatedMainToolProvider struct{ n int }

func (*repeatedMainToolProvider) ListModels(context.Context) ([]string, error) {
	return []string{"main"}, nil
}
func (m *repeatedMainToolProvider) ChatCompletion(_ context.Context, _ string, _ []llm.Message, _ bool) (llm.Response, error) {
	m.n++
	if m.n < 3 {
		return llm.Response{
			Content:   `{"tool":"echo","arguments":{"value":"x"}}`,
			ToolCalls: []llm.ToolCall{{Name: "echo", Arguments: map[string]any{"value": "x"}}},
		}, nil
	}
	return llm.Response{Content: "final answer"}, nil
}

// repeatingToolPlannerProvider executes one operation per planning pass. Each
// pass requires a second call for the tool planner to finish its own turn.
type repeatingToolPlannerProvider struct{ n int }

func (*repeatingToolPlannerProvider) ListModels(context.Context) ([]string, error) {
	return []string{"tool"}, nil
}
func (m *repeatingToolPlannerProvider) ChatCompletion(_ context.Context, _ string, _ []llm.Message, _ bool) (llm.Response, error) {
	m.n++
	if m.n%2 == 1 {
		return llm.Response{ToolCall: &llm.ToolCall{Name: "echo", Arguments: map[string]any{"value": "x"}}}, nil
	}
	return llm.Response{Content: "tool planning complete"}, nil
}
func TestRunToolCycleAndStore(t *testing.T) {
	p := &mockProvider{}
	store := &dialog.Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	out, _, e := (&Engine{Provider: p, Tools: mockTools{}, Store: store, MaxIterations: 2}).Run(context.Background(), "d1", "hello")
	if e != nil || out != "done" || p.n != 2 {
		t.Fatalf("out=%q calls=%d err=%v", out, p.n, e)
	}
	h, e := store.History("d1", 0)
	if e != nil || len(h) != 4 {
		t.Fatalf("history=%#v err=%v", h, e)
	}
	second := p.requests[1]
	if len(second) != 3 || second[1].Role != "assistant" || second[2].Role != "user" {
		t.Fatalf("unexpected tool continuation: %#v", second)
	}
}

func TestRunReturnsToolFailureToModel(t *testing.T) {
	provider := &toolFailureProvider{}
	out, _, err := (&Engine{Provider: provider, Tools: failingTools{}, MaxIterations: 2}).Run(context.Background(), "d1", "read AGENTS.md")
	if err != nil || out != "AGENTS.md is not present in the workspace." || provider.n != 2 {
		t.Fatalf("out=%q calls=%d err=%v", out, provider.n, err)
	}
	if len(provider.requests[1]) < 3 || !strings.Contains(provider.requests[1][2].Content, "openat AGENTS.md") {
		t.Fatalf("tool failure was not sent back: %#v", provider.requests[1])
	}
}

func TestRunPersistsStreamingUsageAsContextSize(t *testing.T) {
	store := &dialog.Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	_, usage, err := (&Engine{Provider: streamingUsageProvider{}, Store: store}).Run(context.Background(), "d1", "hello")
	if err != nil || usage.LastPromptTokens != 40 || usage.LastCompletionTokens != 8 {
		t.Fatalf("usage=%#v err=%v", usage, err)
	}
	history, err := store.History("d1", 0)
	if err != nil || len(history) != 2 || history[1].ContextSize != 48 || history[1].Tokens != 48 {
		t.Fatalf("history=%#v err=%v", history, err)
	}
}

func TestRunExecutesAllToolCallsFromOneResponse(t *testing.T) {
	provider := &multiToolProvider{}
	store := &dialog.Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	out, _, err := (&Engine{Provider: provider, Tools: mockTools{}, Store: store, MaxIterations: 2}).Run(context.Background(), "d1", "hello")
	if err != nil || out != "done" || provider.n != 2 {
		t.Fatalf("out=%q calls=%d err=%v", out, provider.n, err)
	}
	history, err := store.History("d1", 0)
	if err != nil || len(history) != 6 {
		t.Fatalf("history=%#v err=%v", history, err)
	}
	if history[1].Role != "tool_call" || history[3].Role != "tool_call" || history[2].Role != "tool_result" || history[4].Role != "tool_result" {
		t.Fatalf("unexpected multi-call history order: %#v", history)
	}
}

func TestAddUsageAccumulatesIterationsAndKeepsLastContextSnapshot(t *testing.T) {
	usage := addUsage(llm.Usage{}, llm.Usage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120})
	usage = addUsage(usage, llm.Usage{PromptTokens: 180, CompletionTokens: 30})

	if usage.TotalTokens != 330 {
		t.Fatalf("total tokens = %d, want 330", usage.TotalTokens)
	}
	if usage.PromptTokens != 280 || usage.CompletionTokens != 50 {
		t.Fatalf("aggregate usage = %#v", usage)
	}
	if usage.LastPromptTokens != 180 || usage.LastCompletionTokens != 30 {
		t.Fatalf("last context usage = %#v", usage)
	}
}

func TestRunUsesDedicatedToolModelBeforeMainModel(t *testing.T) {
	main := &mainToolRequestProvider{}
	planner := &toolPlannerProvider{}
	store := &dialog.Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	engine := &Engine{
		Provider:         main,
		ToolProvider:     planner,
		Tools:            mockTools{},
		Store:            store,
		ToolDefinitions:  []llm.ToolDefinition{{Name: "echo"}},
		Model:            "main-model",
		ProviderName:     "Main provider",
		ToolModel:        "tool-model",
		ToolProviderName: "Tool provider",
	}
	out, _, err := engine.Run(context.Background(), "d1", "analyze")
	if err != nil || out != "main model answer" || planner.n != 2 || len(main.requests) != 2 {
		t.Fatalf("out=%q planner_calls=%d main_calls=%d err=%v", out, planner.n, len(main.requests), err)
	}
	if got := main.requests[1]; len(got) != 3 || got[1].Role != "assistant" || got[2].Role != "user" {
		t.Fatalf("main model did not receive tool result: %#v", got)
	}
	history, err := store.History("d1", 0)
	if err != nil || len(history) != 4 || history[1].Model != "tool-model" || history[3].Model != "main-model" {
		t.Fatalf("history=%#v err=%v", history, err)
	}
}

func TestRunContinuesDedicatedToolCyclesUntilMainFinalResponse(t *testing.T) {
	main := &repeatedMainToolProvider{}
	planner := &repeatingToolPlannerProvider{}
	store := &dialog.Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	engine := &Engine{
		Provider:         main,
		ToolProvider:     planner,
		Tools:            mockTools{},
		Store:            store,
		ToolDefinitions:  []llm.ToolDefinition{{Name: "echo"}},
		MaxIterations:    2,
		Model:            "main-model",
		ProviderName:     "Main provider",
		ToolModel:        "tool-model",
		ToolProviderName: "Tool provider",
	}
	out, _, err := engine.Run(context.Background(), "d1", "analyze")
	if err != nil || out != "final answer" || main.n != 3 || planner.n != 4 {
		t.Fatalf("out=%q main_calls=%d planner_calls=%d err=%v", out, main.n, planner.n, err)
	}
	history, err := store.History("d1", 0)
	if err != nil || len(history) != 6 {
		t.Fatalf("history=%#v err=%v", history, err)
	}
	if history[1].Role != "tool_call" || history[3].Role != "tool_call" || history[5].Content != "final answer" {
		t.Fatalf("unexpected persisted sequence: %#v", history)
	}
}

func TestRunDoesNotUseToolModelWithoutMainToolCall(t *testing.T) {
	main := &finalProvider{}
	planner := &toolPlannerProvider{}
	out, _, err := (&Engine{
		Provider:        main,
		ToolProvider:    planner,
		Tools:           mockTools{},
		ToolDefinitions: []llm.ToolDefinition{{Name: "echo"}},
	}).Run(context.Background(), "d1", "hello")
	if err != nil || out != "main model answer" || planner.n != 0 || len(main.requests) != 1 {
		t.Fatalf("out=%q planner_calls=%d main_calls=%d err=%v", out, planner.n, len(main.requests), err)
	}
}

func TestRunReturnsMainProviderFailure(t *testing.T) {
	_, _, err := (&Engine{Provider: failingProvider{}}).Run(context.Background(), "d1", "hello")
	if err == nil {
		t.Fatal("expected main provider error")
	}
}

func TestRunReturnsEmptyMainResponse(t *testing.T) {
	_, _, err := (&Engine{Provider: emptyProvider{}}).Run(context.Background(), "d1", "hello")
	if err == nil {
		t.Fatal("expected empty response error")
	}
}

func TestRunDoesNotRetryAfterProviderFailure(t *testing.T) {
	_, _, err := (&Engine{Provider: failingProvider{}}).Run(context.Background(), "d1", "hello")
	if err == nil {
		t.Fatal("expected provider error")
	}
}

func TestRunRejectsEmptyFinalResponse(t *testing.T) {
	_, _, err := (&Engine{Provider: emptyProvider{}}).Run(context.Background(), "d1", "hello")
	if err == nil || err.Error() != "provider returned an empty final response" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFailureDoesNotPersistTechnicalError(t *testing.T) {
	store := &dialog.Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	_, _, err := (&Engine{Provider: failingProvider{}, Store: store}).Run(context.Background(), "d1", "hello")
	if err == nil {
		t.Fatal("expected provider failure")
	}
	history, historyErr := store.History("d1", 0)
	if historyErr != nil || len(history) != 1 || history[0].Role != "user" {
		t.Fatalf("history=%#v err=%v", history, historyErr)
	}
}

func TestRunPlacesSystemContextBeforeHistory(t *testing.T) {
	store := &dialog.Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	if err := store.Append("d1", dialog.Message{Role: "user", Content: "earlier request"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append("d1", dialog.Message{Role: "assistant", Content: "earlier answer"}); err != nil {
		t.Fatal(err)
	}
	provider := &finalProvider{}
	_, _, err := (&Engine{
		Provider:      provider,
		Store:         store,
		SystemContext: "workspace instructions",
	}).Run(context.Background(), "d1", "new request")
	if err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 1 {
		t.Fatalf("requests=%#v", provider.requests)
	}
	got := provider.requests[0]
	if len(got) != 4 || got[0].Role != "system" || got[0].Content != "workspace instructions" || got[1].Role != "user" || got[2].Role != "assistant" || got[3].Role != "user" {
		t.Fatalf("messages=%#v", got)
	}
}

func TestRunResumeDoesNotDuplicateUserMessage(t *testing.T) {
	store := &dialog.Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	if err := store.Append("d1", dialog.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Append("d1", dialog.Message{Role: "assistant", Content: "provider failed", Error: true}); err != nil {
		t.Fatal(err)
	}
	provider := &finalProvider{}
	out, _, err := (&Engine{Provider: provider, Store: store, Resume: true}).Run(context.Background(), "d1", "hello")
	if err != nil || out != "main model answer" || len(provider.requests) != 1 || len(provider.requests[0]) != 1 {
		t.Fatalf("out=%q requests=%#v err=%v", out, provider.requests, err)
	}
	history, historyErr := store.History("d1", 0)
	if historyErr != nil || len(history) != 3 || history[0].Role != "user" || history[2].Role != "assistant" {
		t.Fatalf("history=%#v err=%v", history, historyErr)
	}
}

func TestRunRetriesEmptyFinalResponseAfterToolAndPersistsIt(t *testing.T) {
	p := &retryProvider{}
	store := &dialog.Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	out, _, err := (&Engine{Provider: p, Tools: mockTools{}, Store: store}).Run(context.Background(), "d1", "analyze")
	if err != nil || out != "final after retry" || p.n != 3 {
		t.Fatalf("out=%q calls=%d err=%v", out, p.n, err)
	}
	history, err := store.History("d1", 0)
	if err != nil || len(history) != 4 || history[3].Content != "final after retry" {
		t.Fatalf("history=%#v err=%v", history, err)
	}
}

func TestRunResumeIncludesPersistedToolResult(t *testing.T) {
	store := &dialog.Store{Root: filepath.Join(t.TempDir(), "dialogs")}
	for _, message := range []dialog.Message{
		{Role: "user", Content: "Choose an audit scope"},
		{Role: "assistant", Content: "Please choose one option."},
		{Role: "tool_call", Tool: "user.choice"},
		{Role: "tool_result", Tool: "user.choice", Content: `{"selection":"security"}`},
	} {
		if err := store.Append("d1", message); err != nil {
			t.Fatal(err)
		}
	}
	provider := &finalProvider{}
	if _, _, err := (&Engine{Provider: provider, Store: store, Resume: true}).Run(context.Background(), "d1", ""); err != nil {
		t.Fatal(err)
	}
	if len(provider.requests) != 1 || len(provider.requests[0]) != 3 {
		t.Fatalf("messages=%#v", provider.requests)
	}
	last := provider.requests[0][2]
	if last.Role != "user" || !strings.Contains(last.Content, "user.choice") || !strings.Contains(last.Content, "security") {
		t.Fatalf("tool result message=%#v", last)
	}
}
