package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/statcode-ai/statcode-ai/internal/actor"
	"github.com/statcode-ai/statcode-ai/internal/logger"
	"github.com/statcode-ai/statcode-ai/internal/progress"
)

// ToolExecutionMsg requests execution of a tool call.
type ToolExecutionMsg struct {
	Call               *ToolCall
	ToolName           string
	Approved           bool
	Context            context.Context
	ProgressCallback   progress.Callback
	ToolCallCallback   func(string, string, map[string]interface{}) error
	ToolResultCallback func(string, string, string, string) error
	Heartbeat          time.Duration
	ResponseChannel    chan *ToolResult
}

// Type implements actor.Message.
func (m ToolExecutionMsg) Type() string {
	return "ToolExecutionMsg"
}

// ToolExecutorUpdateRegistryMsg updates the registry used by the executor.
type ToolExecutorUpdateRegistryMsg struct {
	Registry *Registry
}

// Type implements actor.Message.
func (m ToolExecutorUpdateRegistryMsg) Type() string {
	return "ToolExecutorUpdateRegistryMsg"
}

// ToolExecutorActor serializes tool execution through the actor system.
type ToolExecutorActor struct {
	id               string
	registry         *Registry
	defaultHeartbeat time.Duration
}

// NewToolExecutorActor creates a new actor that executes tool calls.
func NewToolExecutorActor(id string, registry *Registry) *ToolExecutorActor {
	return &ToolExecutorActor{
		id:               id,
		registry:         registry,
		defaultHeartbeat: 500 * time.Millisecond,
	}
}

// ID returns the actor ID.
func (a *ToolExecutorActor) ID() string {
	return a.id
}

// Start initializes the actor.
func (a *ToolExecutorActor) Start(ctx context.Context) error {
	return nil
}

// Stop shuts down the actor.
func (a *ToolExecutorActor) Stop(ctx context.Context) error {
	return nil
}

// Receive handles incoming messages.
func (a *ToolExecutorActor) Receive(ctx context.Context, msg actor.Message) error {
	switch m := msg.(type) {
	case ToolExecutionMsg:
		if m.Call == nil || m.ResponseChannel == nil {
			return fmt.Errorf("tool execution message missing call or response channel")
		}
		go a.executeTool(ctx, m)
		return nil
	case ToolExecutorUpdateRegistryMsg:
		if m.Registry == nil {
			return fmt.Errorf("tool executor received nil registry update")
		}
		a.registry = m.Registry
		return nil
	default:
		return fmt.Errorf("tool executor received unknown message type: %T", msg)
	}
}

func (a *ToolExecutorActor) executeTool(actorCtx context.Context, msg ToolExecutionMsg) {
	baseCtx := actorCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	execCtx, cancel := context.WithCancel(baseCtx)
	defer cancel()

	if msg.Context != nil {
		reqCtx := msg.Context
		go func() {
			select {
			case <-execCtx.Done():
			case <-reqCtx.Done():
				cancel()
			}
		}()
	}

	resultChan := make(chan *ToolResult, 1)

	sendProgress := func(update progress.Update) {
		if update.Message == "" && !update.ShouldStatus() {
			return
		}
		if err := progress.Dispatch(msg.ProgressCallback, update); err != nil {
			logger.Debug("tool executor: progress callback error: %v", err)
		}
	}

	go func() {
		resultChan <- a.registry.ExecuteWithCallbacks(execCtx, msg.Call, msg.ToolName, msg.ProgressCallback, msg.ToolCallCallback, msg.ToolResultCallback, msg.Approved)
	}()

	heartbeatInterval := msg.Heartbeat
	if heartbeatInterval <= 0 {
		heartbeatInterval = a.defaultHeartbeat
	}

	var ticker *time.Ticker
	if msg.ProgressCallback != nil && heartbeatInterval > 0 {
		ticker = time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
	}

	heartbeatCount := 0
	for {
		select {
		case result := <-resultChan:
			msg.ResponseChannel <- result
			return
		case <-tickerTick(ticker):
			if msg.ProgressCallback != nil {
				status := fmt.Sprintf("Calling tool: %s%s", msg.ToolName, strings.Repeat(".", heartbeatCount%4))
				sendProgress(progress.Update{
					Message:   status,
					Mode:      progress.ReportJustStatus,
					Ephemeral: true,
				})
				heartbeatCount++
			}
		case <-execCtx.Done():
			msg.ResponseChannel <- &ToolResult{
				ID:    msg.Call.ID,
				Error: "Tool execution cancelled",
			}
			return
		}
	}
}

func tickerTick(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

// ToolExecutorActorClient provides a facade for interacting with ToolExecutorActor.
type ToolExecutorActorClient struct {
	actorRef interface {
		Send(actor.Message) error
	}
}

// NewToolExecutorActorClient returns a new client.
func NewToolExecutorActorClient(ref interface{ Send(actor.Message) error }) *ToolExecutorActorClient {
	return &ToolExecutorActorClient{actorRef: ref}
}

// Execute runs the tool call through the actor.
func (c *ToolExecutorActorClient) Execute(ctx context.Context, call *ToolCall, toolName string, progressCallback progress.Callback) (*ToolResult, error) {
	return c.execute(ctx, call, toolName, progressCallback, nil, nil, false)
}

// ExecuteWithApproval runs the tool call with prior approval.
func (c *ToolExecutorActorClient) ExecuteWithApproval(ctx context.Context, call *ToolCall, toolName string, progressCallback progress.Callback) (*ToolResult, error) {
	return c.execute(ctx, call, toolName, progressCallback, nil, nil, true)
}

// ExecuteWithCallbacks runs the tool call with optional callbacks and optional approval bypass.
func (c *ToolExecutorActorClient) ExecuteWithCallbacks(ctx context.Context, call *ToolCall, toolName string, progressCallback progress.Callback, toolCallCb func(string, string, map[string]interface{}) error, toolResultCb func(string, string, string, string) error, approved bool) (*ToolResult, error) {
	return c.execute(ctx, call, toolName, progressCallback, toolCallCb, toolResultCb, approved)
}

// SetRegistry updates the executor's registry.
func (c *ToolExecutorActorClient) SetRegistry(reg *Registry) error {
	if reg == nil {
		return fmt.Errorf("registry must not be nil")
	}
	return c.actorRef.Send(ToolExecutorUpdateRegistryMsg{Registry: reg})
}

func (c *ToolExecutorActorClient) execute(ctx context.Context, call *ToolCall, toolName string, progressCallback progress.Callback, toolCallCb func(string, string, map[string]interface{}) error, toolResultCb func(string, string, string, string) error, approved bool) (*ToolResult, error) {
	if call == nil {
		return nil, fmt.Errorf("tool call is nil")
	}

	respChan := make(chan *ToolResult, 1)
	msg := ToolExecutionMsg{
		Call:               call,
		ToolName:           toolName,
		Approved:           approved,
		Context:            ctx,
		ProgressCallback:   progressCallback,
		ToolCallCallback:   toolCallCb,
		ToolResultCallback: toolResultCb,
		ResponseChannel:    respChan,
	}

	if err := c.actorRef.Send(msg); err != nil {
		return nil, err
	}

	if ctx == nil {
		return <-respChan, nil
	}

	select {
	case res := <-respChan:
		return res, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
