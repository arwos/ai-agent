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
