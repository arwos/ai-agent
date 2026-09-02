package workspace

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestRegistryIsolatesParallelWorkspaces(t *testing.T) {
	r := NewRegistry()
	a, e := r.Open("a", filepath.Join(t.TempDir(), "a"))
	if e != nil {
		t.Fatal(e)
	}
	b, e := r.Open("b", filepath.Join(t.TempDir(), "b"))
	if e != nil {
		t.Fatal(e)
	}
	_ = a
	_ = b
	var wg sync.WaitGroup
	for id, content := range map[string]string{"a": "first", "b": "second"} {
		wg.Add(1)
		go func(id, content string) {
			defer wg.Done()
			s, e := r.Get(id)
			if e != nil {
				t.Error(e)
				return
			}
			if e = s.WriteFile("same.txt", content); e != nil {
				t.Error(e)
			}
		}(id, content)
	}
	wg.Wait()
	for id, want := range map[string]string{"a": "first", "b": "second"} {
		s, e := r.Get(id)
		if e != nil {
			t.Fatal(e)
		}
		got, e := s.ReadFile("same.txt")
		if e != nil || got != want {
			t.Fatalf("%s: %q %v", id, got, e)
		}
	}
}

func TestOpenNextAllowsSameBasenameForDifferentDirectories(t *testing.T) {
	r := NewRegistry()
	root := t.TempDir()
	firstPath, secondPath := filepath.Join(root, "one", "app"), filepath.Join(root, "two", "app")
	for _, path := range []string{firstPath, secondPath} {
		if err := os.MkdirAll(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	first, err := r.OpenNext(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.OpenNext(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID {
		t.Fatalf("workspace IDs must differ: %#v %#v", first, second)
	}
	if first.ID != "app" || second.ID != "app-2" {
		t.Fatalf("unexpected IDs: %#v %#v", first, second)
	}
	reopened, err := r.OpenNext(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ID != first.ID {
		t.Fatalf("same path should reuse workspace %q, got %q", first.ID, reopened.ID)
	}
}
