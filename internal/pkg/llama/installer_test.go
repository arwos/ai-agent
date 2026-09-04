package llama

import "testing"

func TestValidModelID(t *testing.T) {
	for _, value := range []string{
		"ggml-org/Nemotron-3-Nano-4B-GGUF:Q8_0",
		"ggml-org/Qwen3-0.6B-GGUF:Q8_0",
		"ggml-org/model",
	} {
		if !validModelID(value) {
			t.Errorf("validModelID(%q) = false", value)
		}
	}
	for _, value := range []string{"ggml-org/model:Q8_0:extra", "ggml-org/:Q8_0", "../model"} {
		if validModelID(value) {
			t.Errorf("validModelID(%q) = true", value)
		}
	}
}

func TestModelRepoID(t *testing.T) {
	if got := modelRepoID("ggml-org/Qwen3-0.6B-GGUF:Q8_0"); got != "ggml-org/Qwen3-0.6B-GGUF" {
		t.Fatalf("modelRepoID() = %q", got)
	}
}
