package orchestrator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/codefionn/scriptschnell/internal/progress"
	"github.com/codefionn/scriptschnell/internal/session"
	"github.com/codefionn/scriptschnell/internal/tools"
)

func TestProcessToolCallsPreservesOrder(t *testing.T) {
	ctx := context.Background()
	sess := session.NewSession("test", ".")

	toolCalls := []map[string]interface{}{
		{
			"id":   "call-1",
			"type": "function",
			"function": map[string]interface{}{
				"name":      "fast-tool",
				"arguments": "{}",
			},
		},
		{
			"id":   "call-2",
			"type": "function",
			"function": map[string]interface{}{
				"name":      "slow-tool",
				"arguments": "{}",
			},
		},
	}

	orch := &Orchestrator{}

	// Run calls with different latencies to ensure ordering is dictated by the
	// original call index, not completion time.
	execFn := func(ctx context.Context, call *tools.ToolCall, toolName string, progressCb progress.Callback, toolCallCb ToolCallCallback, toolResultCb ToolResultCallback, approved bool) (*tools.ToolResult, error) {
		if toolName == "slow-tool" {
			time.Sleep(15 * time.Millisecond)
		}
		return &tools.ToolResult{
			ID:     call.ID,
			Result: fmt.Sprintf("result-%s", call.ID),
		}, nil
	}

	if err := orch.processToolCalls(ctx, toolCalls, sess, nil, nil, nil, nil, execFn); err != nil {
		t.Fatalf("processToolCalls returned error: %v", err)
	}

	messages := sess.GetMessages()
	if len(messages) != 2 {
		t.Fatalf("expected 2 tool messages, got %d", len(messages))
	}

	if messages[0].ToolID != "call-1" || messages[0].ToolName != "fast-tool" || messages[0].Content != "result-call-1" {
		t.Fatalf("unexpected first tool message: %+v", messages[0])
	}

	if messages[1].ToolID != "call-2" || messages[1].ToolName != "slow-tool" || messages[1].Content != "result-call-2" {
		t.Fatalf("unexpected second tool message: %+v", messages[1])
	}
}
