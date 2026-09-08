package toolexecutor

import (
	"context"
	"strings"
	"testing"
)

func TestScopedAliasAllowsOnlyRegisteredBuiltinTool(t *testing.T) {
	r := New()
	if err := r.Register(Tool{Name: "workspace.read", Description: "read", Handler: func(context.Context, Scope, map[string]any) (any, error) { return "ok", nil }}); err != nil {
		t.Fatal(err)
	}
	scope := Scope{Aliases: map[string]string{"fs.open": "workspace.read"}, Allowed: map[string]bool{"workspace.read": true}}
	value, err := r.Execute(context.Background(), scope, "fs.open", nil)
	if err != nil || value != `"ok"` {
		t.Fatalf("value=%q err=%v", value, err)
	}
	_, err = r.Execute(context.Background(), scope, "workspace.read", nil)
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected unavailable error, got %v", err)
	}
}

func TestExecuteRequiresApproval(t *testing.T) {
	r := New()
	if err := r.Register(Tool{Name: "fs.delete_file", Description: "delete", RequiresApproval: true, Handler: func(context.Context, Scope, map[string]any) (any, error) { return "deleted", nil }}); err != nil {
		t.Fatal(err)
	}
	scope := Scope{Allowed: map[string]bool{"fs.delete_file": true}, Request: func(context.Context, map[string]any) (any, error) { return false, nil }}
	if _, err := r.Execute(context.Background(), scope, "fs.delete_file", map[string]any{"path": "a"}); err == nil {
		t.Fatal("declined tool was executed")
	}
	scope.Request = func(context.Context, map[string]any) (any, error) { return true, nil }
	if value, err := r.Execute(context.Background(), scope, "fs.delete_file", map[string]any{"path": "a"}); err != nil || value != `"deleted"` {
		t.Fatalf("approved execution = %q, %v", value, err)
	}
}

func TestConsultAllowsOnlyReadOnlyAndControlTools(t *testing.T) {
	r := New()
	for _, name := range []string{"agent.list", "agent.run", "agent.result", "fs.read_file"} {
		name := name
		if err := r.Register(Tool{Name: name, Description: name, Handler: func(context.Context, Scope, map[string]any) (any, error) { return "ok", nil }}); err != nil {
			t.Fatal(err)
		}
	}
	scope := Scope{Mode: "consult", Allowed: map[string]bool{"agent.list": true, "agent.run": true, "agent.result": true, "fs.read_file": true}}
	for _, name := range []string{"agent.list", "agent.run", "agent.result", "fs.read_file"} {
		if _, err := r.Execute(context.Background(), scope, name, nil); err != nil {
			t.Errorf("consult tool %q rejected: %v", name, err)
		}
	}
	if _, err := r.Execute(context.Background(), scope, "fs.delete_file", nil); err == nil {
		t.Fatal("consult mode allowed a non-read-only tool")
	}
}

func TestDesignAllowsOnlyConfigurationTools(t *testing.T) {
	r := New()
	for _, name := range []string{"workflow.get", "skills.search", "agent.list", "fs.write_file"} {
		name := name
		if err := r.Register(Tool{Name: name, Description: name, Handler: func(context.Context, Scope, map[string]any) (any, error) { return "ok", nil }}); err != nil {
			t.Fatal(err)
		}
	}
	scope := Scope{Mode: "design", Allowed: map[string]bool{"workflow.get": true, "skills.search": true, "agent.list": true, "fs.write_file": true}}
	for _, name := range []string{"workflow.get", "skills.search", "agent.list"} {
		if _, err := r.Execute(context.Background(), scope, name, nil); err != nil {
			t.Errorf("design tool %q rejected: %v", name, err)
		}
	}
	if _, err := r.Execute(context.Background(), scope, "fs.write_file", nil); err == nil {
		t.Fatal("design mode allowed workspace mutation")
	}
}

func TestPlanRejectsToolExecution(t *testing.T) {
	r := New()
	if err := r.Register(Tool{Name: "fs.write_file", Description: "write", Handler: func(context.Context, Scope, map[string]any) (any, error) { return "ok", nil }}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Execute(context.Background(), Scope{Mode: "plan", Allowed: map[string]bool{"fs.write_file": true}}, "fs.write_file", nil); err == nil {
		t.Fatal("plan mode allowed tool execution")
	}
}
