package llm

import "testing"

func TestModelSpecificPrompt_MistralFamily(t *testing.T) {
	pb := &PromptBuilder{}

	got := pb.modelSpecificPrompt("mistral-large", []map[string]interface{}{})
	if got == "" {
		t.Fatalf("expected modelSpecificPrompt to return text for mistral models")
	}
	if got != "Shell commands can be executed with the golang sandbox tool call.\n" {
		t.Fatalf("unexpected modelSpecificPrompt for mistral: %q", got)
	}
}
