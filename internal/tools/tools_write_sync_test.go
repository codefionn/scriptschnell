package tools

import (
	"context"
	"sync"
	"testing"
	"time"
)

type blockingWriteTool struct {
	start   chan struct{}
	proceed chan struct{}
}

func newBlockingWriteTool() *blockingWriteTool {
	return &blockingWriteTool{
		start:   make(chan struct{}, 2),
		proceed: make(chan struct{}, 2),
	}
}

func (t *blockingWriteTool) Name() string        { return ToolNameEditFile }
func (t *blockingWriteTool) Description() string { return "blocking write tool" }
func (t *blockingWriteTool) Parameters() map[string]interface{} {
	return map[string]interface{}{}
}
func (t *blockingWriteTool) RequiresExclusiveExecution() bool { return true }

func (t *blockingWriteTool) Execute(ctx context.Context, params map[string]interface{}) *ToolResult {
	select {
	case t.start <- struct{}{}:
	default:
	}

	select {
	case <-t.proceed:
	case <-ctx.Done():
		return &ToolResult{Error: ctx.Err().Error()}
	}

	return &ToolResult{Result: "done"}
}

func TestRegistrySerializesEditFileCalls(t *testing.T) {
	registry := NewRegistry(nil)
	tool := newBlockingWriteTool()
	registry.RegisterSpec(tool, func(*Registry) ToolExecutor { return tool })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		registry.Execute(ctx, &ToolCall{ID: "1", Name: ToolNameEditFile, Parameters: map[string]interface{}{}})
	}()

	go func() {
		defer wg.Done()
		registry.Execute(ctx, &ToolCall{ID: "2", Name: ToolNameEditFile, Parameters: map[string]interface{}{}})
	}()

	waitForStart := func() {
		t.Helper()
		select {
		case <-tool.start:
		case <-ctx.Done():
			t.Fatalf("timed out waiting for tool start: %v", ctx.Err())
		}
	}

	waitForStart()

	select {
	case <-tool.start:
		t.Fatal("second edit_file call started before first completed")
	default:
	}

	tool.proceed <- struct{}{}

	waitForStart()
	tool.proceed <- struct{}{}

	wg.Wait()
}
