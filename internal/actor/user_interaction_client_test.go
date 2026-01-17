package actor

import (
	"context"
	"testing"
	"time"
)

func setupTestActorWithHandler(t *testing.T, handler UserInteractionHandler) (*ActorRef, *UserInteractionClient, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())

	actor := NewUserInteractionActor("test-actor", handler)
	system := NewSystem()

	ref, err := system.Spawn(ctx, "test-actor", actor, 16)
	if err != nil {
		cancel()
		t.Fatalf("Failed to spawn actor: %v", err)
	}

	client := NewUserInteractionClient(ref)

	return ref, client, cancel
}

func TestUserInteractionClientRequestAuthorization(t *testing.T) {
	handler := newMockHandler("test")
	handler.handleFunc = func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
		payload := req.Payload.(*AuthorizationPayload)
		// Approve only git commands
		approved := payload.ToolName == "shell" && payload.Parameters["command"] == "git status"
		return &UserInteractionResponse{
			RequestID:    req.RequestID,
			Approved:     approved,
			Acknowledged: true,
		}, nil
	}

	_, client, cancel := setupTestActorWithHandler(t, handler)
	defer cancel()

	ctx := context.Background()

	// Test approved request
	resp, err := client.RequestAuthorization(ctx, "shell", map[string]interface{}{"command": "git status"}, "run git", "", 0)
	if err != nil {
		t.Fatalf("RequestAuthorization failed: %v", err)
	}
	if !resp.Approved {
		t.Error("Expected Approved for git status")
	}

	// Test denied request
	resp, err = client.RequestAuthorization(ctx, "shell", map[string]interface{}{"command": "rm -rf /"}, "dangerous", "", 0)
	if err != nil {
		t.Fatalf("RequestAuthorization failed: %v", err)
	}
	if resp.Approved {
		t.Error("Expected not Approved for dangerous command")
	}
}

func TestUserInteractionClientRequestDomainAuthorization(t *testing.T) {
	handler := newMockHandler("test")
	handler.handleFunc = func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
		payload := req.Payload.(*AuthorizationPayload)
		// Approve only github.com
		domain := payload.Parameters["domain"].(string)
		approved := domain == "github.com"
		return &UserInteractionResponse{
			RequestID:    req.RequestID,
			Approved:     approved,
			Acknowledged: true,
		}, nil
	}

	_, client, cancel := setupTestActorWithHandler(t, handler)
	defer cancel()

	ctx := context.Background()

	// Test approved request
	resp, err := client.RequestDomainAuthorization(ctx, "github.com", "access github", "", 0)
	if err != nil {
		t.Fatalf("RequestDomainAuthorization failed: %v", err)
	}
	if !resp.Approved {
		t.Error("Expected Approved for github.com")
	}

	// Test denied request
	resp, err = client.RequestDomainAuthorization(ctx, "evil.com", "access evil", "", 0)
	if err != nil {
		t.Fatalf("RequestDomainAuthorization failed: %v", err)
	}
	if resp.Approved {
		t.Error("Expected not Approved for evil.com")
	}
}

func TestUserInteractionClientRequestUserInput(t *testing.T) {
	handler := newMockHandler("test")
	handler.handleFunc = func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
		payload := req.Payload.(*UserInputSinglePayload)
		return &UserInteractionResponse{
			RequestID:    req.RequestID,
			Answer:       "answer to: " + payload.Question,
			Acknowledged: true,
		}, nil
	}

	_, client, cancel := setupTestActorWithHandler(t, handler)
	defer cancel()

	ctx := context.Background()

	resp, err := client.RequestUserInput(ctx, "What is your name?", "", 0)
	if err != nil {
		t.Fatalf("RequestUserInput failed: %v", err)
	}
	if resp.Answer != "answer to: What is your name?" {
		t.Errorf("Unexpected answer: %s", resp.Answer)
	}
}

func TestUserInteractionClientRequestMultipleAnswers(t *testing.T) {
	handler := newMockHandler("test")
	handler.handleFunc = func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
		return &UserInteractionResponse{
			RequestID: req.RequestID,
			Answers: map[string]string{
				"q1": "a1",
				"q2": "a2",
			},
			Acknowledged: true,
		}, nil
	}

	_, client, cancel := setupTestActorWithHandler(t, handler)
	defer cancel()

	ctx := context.Background()

	questions := []QuestionWithOptions{
		{Question: "q1", Options: []string{"a1", "b1"}},
		{Question: "q2", Options: []string{"a2", "b2"}},
	}

	resp, err := client.RequestMultipleAnswers(ctx, "formatted", questions, 0)
	if err != nil {
		t.Fatalf("RequestMultipleAnswers failed: %v", err)
	}
	if resp.Answers["q1"] != "a1" || resp.Answers["q2"] != "a2" {
		t.Errorf("Unexpected answers: %v", resp.Answers)
	}
}

func TestUserInteractionClientRequestPlanningQuestion(t *testing.T) {
	handler := newMockHandler("test")
	handler.handleFunc = func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
		payload := req.Payload.(*PlanningQuestionPayload)
		return &UserInteractionResponse{
			RequestID:    req.RequestID,
			Answer:       "planning: " + payload.Question,
			Acknowledged: true,
		}, nil
	}

	_, client, cancel := setupTestActorWithHandler(t, handler)
	defer cancel()

	ctx := context.Background()

	resp, err := client.RequestPlanningQuestion(ctx, "What approach?", "context", 0)
	if err != nil {
		t.Fatalf("RequestPlanningQuestion failed: %v", err)
	}
	if resp.Answer != "planning: What approach?" {
		t.Errorf("Unexpected answer: %s", resp.Answer)
	}
}

func TestUserInteractionClientContextCancellation(t *testing.T) {
	handler := newMockHandler("test")
	handler.handleFunc = func(ctx context.Context, req *UserInteractionRequest) (*UserInteractionResponse, error) {
		// Wait for context cancellation
		<-ctx.Done()
		return &UserInteractionResponse{
			RequestID: req.RequestID,
			Cancelled: true,
		}, ctx.Err()
	}

	_, client, cancel := setupTestActorWithHandler(t, handler)
	defer cancel()

	ctx, ctxCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer ctxCancel()

	resp, err := client.RequestAuthorization(ctx, "shell", nil, "test", "", 0)
	if err == nil && !resp.Cancelled && !resp.TimedOut {
		t.Error("Expected cancellation or timeout")
	}
}

func TestUserInteractionClientSetDefaultTimeout(t *testing.T) {
	handler := newMockHandler("test")
	_, client, cancel := setupTestActorWithHandler(t, handler)
	defer cancel()

	client.SetDefaultTimeout(5 * time.Second)
	// Just verify it doesn't panic - the actual timeout is tested in actor tests
}

func TestUserInteractionClientCancel(t *testing.T) {
	handler := newMockHandler("test")
	ref, client, cancel := setupTestActorWithHandler(t, handler)
	defer cancel()

	// Cancel should not error on valid actor ref
	err := client.Cancel("some-request-id", "test reason")
	if err != nil {
		t.Errorf("Cancel failed: %v", err)
	}

	_ = ref // Keep reference alive
}

func TestUserInteractionClientAcknowledge(t *testing.T) {
	handler := newMockHandler("test")
	ref, client, cancel := setupTestActorWithHandler(t, handler)
	defer cancel()

	// Acknowledge should not error on valid actor ref
	err := client.Acknowledge("some-request-id")
	if err != nil {
		t.Errorf("Acknowledge failed: %v", err)
	}

	_ = ref // Keep reference alive
}
