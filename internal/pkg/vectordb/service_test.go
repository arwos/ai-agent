package vectordb

import (
	"context"
	"os/exec"
	"slices"
	"testing"
)

func TestServiceStart(t *testing.T) {
	var gotName string
	var gotArgs []string
	service := New("./datasource/vectordb", "127.0.0.1:6334")
	service.commandContext = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return exec.CommandContext(ctx, "go", "version")
	}

	process, err := service.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, err := process.Wait(); err != nil {
		t.Fatalf("fake process error = %v", err)
	}

	if gotName != "docker" {
		t.Fatalf("command name = %q, want docker", gotName)
	}
	wantArgs := []string{"run", "-p", "6333:6333", "-p", "6334:6334", "-v", "./datasource/vectordb:/qdrant/storage", "qdrant/qdrant"}
	if !slices.Equal(gotArgs, wantArgs) {
		t.Fatalf("command args = %#v, want %#v", gotArgs, wantArgs)
	}
}

func TestServiceStartRejectsMissingFolder(t *testing.T) {
	process, err := New(" ", "127.0.0.1:6334").Start(context.Background())
	if process != nil {
		t.Fatal("Start() process is not nil")
	}
	if err == nil || err.Error() != "vectordb folder is required" {
		t.Fatalf("Start() error = %v", err)
	}
}

func TestServiceStartReturnsCommandError(t *testing.T) {
	service := New("./datasource/vectordb", "127.0.0.1:6334")
	service.commandContext = func(context.Context, string, ...string) *exec.Cmd {
		return exec.Command("command-that-does-not-exist")
	}

	process, err := service.Start(context.Background())
	if process != nil {
		t.Fatal("Start() process is not nil")
	}
	if err == nil {
		t.Fatal("Start() error = nil, want process-start error")
	}
}
