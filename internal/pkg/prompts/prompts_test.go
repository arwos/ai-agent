package prompts

import (
	"strings"
	"testing"
)

func TestCompactionSystemForIncludesLevelAndOutputRules(t *testing.T) {
	system := CompactionSystemFor("epic")
	if !strings.Contains(system, "Compression level: EPIC") {
		t.Fatalf("missing selected level: %q", system)
	}
	if !strings.Contains(system, "Return only valid JSON") || !strings.Contains(system, "must be English") {
		t.Fatalf("missing output rules: %q", system)
	}
}

func TestEnvironmentIncludesGitBranchOnlyWhenKnown(t *testing.T) {
	with := Environment("ws", "/tmp/ws", "linux/amd64", "2026-09-05", "main")
	without := Environment("ws", "/tmp/ws", "linux/amd64", "2026-09-05", "")
	if !strings.Contains(with, "- Git branch: main") || strings.Contains(without, "Git branch") {
		t.Fatalf("with=%q without=%q", with, without)
	}
}
