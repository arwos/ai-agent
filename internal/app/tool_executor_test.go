package app

import (
	"context"
	"testing"

	"github.com/arwos/ai-agent/internal/pkg/toolexecutor"
)

func TestAppToolExecutorRunsGateBeforeSideEffect(t *testing.T) {
	called := false
	registry := toolexecutor.New()
	if err := registry.Register(toolexecutor.Tool{
		Name:        "fs.write",
		Description: "write",
		Handler: func(context.Context, toolexecutor.Scope, map[string]any) (any, error) {
			called = true
			return map[string]bool{"ok": true}, nil
		},
	}); err != nil {
		t.Fatal(err)
	}
	executor := appToolExecutor{
		registry: registry,
		before: func(string, map[string]any) error {
			return context.Canceled
		},
	}
	if _, err := executor.Execute(context.Background(), "fs.write", nil); err != context.Canceled {
		t.Fatalf("gate error = %v, want context canceled", err)
	}
	if called {
		t.Fatal("side effect ran after workflow gate rejection")
	}
}
