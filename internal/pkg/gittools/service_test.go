package gittools

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/go-git/go-billy/v6/osfs"
	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/storage/memory"
)

func TestValidateRelativeRejectsEscape(t *testing.T) {
	for _, value := range []string{"", ".", "..", "../secret", "/tmp/secret"} {
		if err := validateRelative(value); err == nil {
			t.Errorf("validateRelative(%q) accepted unsafe path", value)
		}
	}
}

func TestValidateBranchRejectsUnsafeNames(t *testing.T) {
	for _, value := range []string{"", "../main", "feature\\x"} {
		if err := validateBranch(value); err == nil {
			t.Errorf("validateBranch(%q) accepted unsafe name", value)
		}
	}
}

func TestRepositoryOperationsUseRootFilesystem(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	worktree, err := osfs.FromRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	repo, err := git.Init(memory.NewStorage(), git.WithWorkTree(worktree))
	if err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("README.md", []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	wrapped := &Repository{root: root, worktree: worktree, repo: repo}
	if err := wrapped.Add([]string{"README.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := wrapped.Commit("initial"); err != nil {
		t.Fatal(err)
	}
	if err := root.WriteFile("README.md", []byte("hello\nworld\n"), 0644); err != nil {
		t.Fatal(err)
	}
	status, err := wrapped.Status()
	if err != nil {
		t.Fatal(err)
	}
	if len(status) != 1 || status[0].Path != "README.md" {
		t.Fatalf("unexpected status: %#v", status)
	}
	diff, err := wrapped.Diff(context.Background(), false, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" || !containsAll(diff, "--- a/README.md", "+world") {
		t.Fatalf("unexpected diff: %q", diff)
	}
	log, err := wrapped.Log(context.Background(), 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(log) != 1 || log[0].Message != "initial" {
		t.Fatalf("unexpected log: %#v", log)
	}
}

func TestInitCreatesGitDirectoryBelowRoot(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	repo, err := Init(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := root.Stat(".git"); err != nil {
		t.Fatalf(".git is not inside root: %v", err)
	}
	if _, err := Init(root); err == nil {
		t.Fatal("second initialization should fail")
	}
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
