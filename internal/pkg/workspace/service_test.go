package workspace

import (
	"path/filepath"
	"testing"
)

func TestTraversalBlocked(t *testing.T) {
	s, _ := NewService(t.TempDir())
	if _, e := s.ReadFile(filepath.Join("..", "secret")); e != ErrOutsideRoot {
		t.Fatalf("expected traversal error, got %v", e)
	}
}

func TestListFileInfoKeepsDirectoryEntries(t *testing.T) {
	s, err := NewService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.WriteFile("src/main.go", "package main\n"); err != nil {
		t.Fatal(err)
	}
	root, err := s.ListFileInfo(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(root) != 1 || root[0].ID != "src" || !root[0].IsDir {
		t.Fatalf("root entries = %#v", root)
	}
	nested, err := s.ListFileInfo("src")
	if err != nil {
		t.Fatal(err)
	}
	if len(nested) != 1 || nested[0].ID != "src/main.go" || nested[0].IsDir {
		t.Fatalf("nested entries = %#v", nested)
	}
}
