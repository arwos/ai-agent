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
