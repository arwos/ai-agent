package responseparser

import "testing"

func TestFormats(t *testing.T) {
	var stream OpenAIStream
	stream.Add(OpenAIFragment{Index: 0, ID: "call-1", Name: "read_", Arguments: `{"path":`})
	stream.Add(OpenAIFragment{Index: 0, Name: "file", Arguments: `"README.md"}`})
	calls, err := stream.Finish()
	if err != nil || len(calls) != 1 || calls[0].Name != "read_file" || calls[0].Arguments["path"] != "README.md" {
		t.Fatalf("openai calls=%#v err=%v", calls, err)
	}
	anthropic, err := AnthropicContent([]AnthropicBlock{{Type: "tool_use", ID: "toolu-1", Name: "read_file", Input: []byte(`{"path":"README.md"}`)}})
	if err != nil || len(anthropic) != 1 {
		t.Fatalf("anthropic calls=%#v err=%v", anthropic, err)
	}
	gemini, err := GeminiParts([]GeminiPart{{Text: "reading", Thought: true}, {FunctionCall: &struct {
		ID   string         `json:"id"`
		Name string         `json:"name"`
		Args map[string]any `json:"args"`
	}{Name: "read_file", Args: map[string]any{"path": "README.md"}}}})
	if err != nil || len(gemini) != 1 || gemini[0].ID == "" {
		t.Fatalf("gemini calls=%#v err=%v", gemini, err)
	}
}
