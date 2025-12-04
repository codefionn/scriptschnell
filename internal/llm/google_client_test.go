package llm

import (
	"bytes"
	"encoding/base64"
	"testing"

	genai "google.golang.org/genai"
)

func TestGoogleClient_ToolCallThoughtSignatureRoundTrip(t *testing.T) {
	signature := []byte{0xde, 0xad, 0xbe, 0xef}

	part := genai.NewPartFromFunctionCall("do_stuff", map[string]any{"value": "x"})
	part.Thought = true
	part.ThoughtSignature = signature

	content := genai.NewContentFromParts([]*genai.Part{part}, genai.RoleModel)

	toolCalls := convertToolCallsFromContent(content)
	if len(toolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(toolCalls))
	}

	tc := toolCalls[0]
	sig, ok := tc["thought_signature"].(string)
	if !ok || sig == "" {
		t.Fatalf("expected thought_signature to be captured, got %#v", tc["thought_signature"])
	}
	if sig != base64.StdEncoding.EncodeToString(signature) {
		t.Fatalf("expected signature %q, got %q", base64.StdEncoding.EncodeToString(signature), sig)
	}

	assistantMsg, err := convertAssistantMessage(&Message{Role: "assistant", ToolCalls: toolCalls})
	if err != nil {
		t.Fatalf("convertAssistantMessage returned error: %v", err)
	}

	if len(assistantMsg.Parts) != 1 {
		t.Fatalf("expected 1 part after round-trip, got %d", len(assistantMsg.Parts))
	}

	resultPart := assistantMsg.Parts[0]
	if !resultPart.Thought {
		t.Fatalf("expected Thought to be preserved")
	}
	if !bytes.Equal(resultPart.ThoughtSignature, signature) {
		t.Fatalf("expected signature %v, got %v", signature, resultPart.ThoughtSignature)
	}
	if resultPart.FunctionCall == nil || resultPart.FunctionCall.Name != "do_stuff" {
		t.Fatalf("expected function call to be preserved, got %+v", resultPart.FunctionCall)
	}
}
