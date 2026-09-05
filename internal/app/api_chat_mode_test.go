package app

import "testing"

func TestValidateChatMode(t *testing.T) {
	for _, mode := range []string{"normal", "plan", "consult", "workflow"} {
		id := ""
		if mode == "workflow" {
			id = "build"
		}
		if err := validateChatMode(mode, id, false); err != nil {
			t.Errorf("mode %q rejected: %v", mode, err)
		}
	}
	if err := validateChatMode("invalid", "", false); err == nil {
		t.Fatal("invalid mode accepted")
	}
	if err := validateChatMode("workflow", "", false); err == nil {
		t.Fatal("workflow without id accepted")
	}
	if err := validateChatMode("consult", "build", false); err == nil {
		t.Fatal("workflow id accepted outside workflow mode")
	}
}
