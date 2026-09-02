package agent

import (
	"testing"

	"github.com/arwos/ai-agent/internal/pkg/dialog"
)

func TestRegistryResetsReleasedEngine(t *testing.T) {
	r := NewRegistry()
	e := r.Acquire(nil, nil, &dialog.Store{}, "prompt")
	e.MaxIterations = 3
	r.Release(e)
	again := r.Acquire(nil, nil, nil, "")
	if again.MaxIterations != 8 || again.Store != nil || again.SystemPrompt != "" {
		t.Fatalf("engine was not reset: %#v", again)
	}
	r.Release(again)
}
