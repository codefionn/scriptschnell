package acp

import (
	"context"
	"fmt"
	"time"

	"github.com/codefionn/scriptschnell/internal/actor"
	"github.com/codefionn/scriptschnell/internal/logger"
	"github.com/coder/acp-go-sdk"
)

// ACPInteractionHandler implements UserInteractionHandler for ACP mode
type ACPInteractionHandler struct {
	conn      *acp.AgentSideConnection
	sessionID string
}

// NewACPInteractionHandler creates a new ACP interaction handler
func NewACPInteractionHandler(conn *acp.AgentSideConnection, sessionID string) *ACPInteractionHandler {
	return &ACPInteractionHandler{
		conn:      conn,
		sessionID: sessionID,
	}
}

// Mode returns the handler mode name
func (h *ACPInteractionHandler) Mode() string {
	return "acp"
}

// SupportsInteraction indicates whether this handler can handle the given type.
// ACP mode currently supports authorization requests.
func (h *ACPInteractionHandler) SupportsInteraction(interactionType actor.InteractionType) bool {
	switch interactionType {
	case actor.InteractionTypeAuthorization:
		return true
	case actor.InteractionTypePlanningQuestion, actor.InteractionTypeUserInputSingle, actor.InteractionTypeUserInputMultiple:
		// These could be supported via RequestInput when ACP supports it
		return false
	default:
		return false
	}
}

// HandleInteraction processes a user interaction request
func (h *ACPInteractionHandler) HandleInteraction(ctx context.Context, req *actor.UserInteractionRequest) (*actor.UserInteractionResponse, error) {
	switch req.InteractionType {
	case actor.InteractionTypeAuthorization:
		return h.handleAuthorization(ctx, req)
	default:
		logger.Debug("ACPInteractionHandler: unsupported interaction type %s", req.InteractionType)
		return &actor.UserInteractionResponse{
			RequestID:    req.RequestID,
			Approved:     false,
			Acknowledged: true,
			Error:        fmt.Errorf("ACP mode does not support %s interactions", req.InteractionType),
		}, nil
	}
}

// handleAuthorization handles authorization requests via ACP RequestPermission
func (h *ACPInteractionHandler) handleAuthorization(ctx context.Context, req *actor.UserInteractionRequest) (*actor.UserInteractionResponse, error) {
	payload, ok := req.Payload.(*actor.AuthorizationPayload)
	if !ok {
		return nil, fmt.Errorf("invalid payload type for authorization: expected *AuthorizationPayload, got %T", req.Payload)
	}

	logger.Debug("ACPInteractionHandler: requesting permission for tool=%s", payload.ToolName)

	// Determine tool kind based on tool name
	toolKind := h.getToolKind(payload.ToolName, payload.Parameters)

	// Extract file locations from parameters
	locations := h.extractLocations(payload.ToolName, payload.Parameters)

	permResp, err := h.conn.RequestPermission(ctx, acp.RequestPermissionRequest{
		SessionId: acp.SessionId(h.sessionID),
		ToolCall: acp.RequestPermissionToolCall{
			ToolCallId: acp.ToolCallId(fmt.Sprintf("auth_%d", time.Now().UnixNano())),
			Title:      acp.Ptr(fmt.Sprintf("Execute %s", payload.ToolName)),
			Kind:       acp.Ptr(toolKind),
			Status:     acp.Ptr(acp.ToolCallStatusPending),
			Locations:  locations,
			RawInput:   payload.Parameters,
		},
		Options: []acp.PermissionOption{
			{Kind: acp.PermissionOptionKindAllowOnce, Name: "Allow", OptionId: acp.PermissionOptionId("allow")},
			{Kind: acp.PermissionOptionKindRejectOnce, Name: "Deny", OptionId: acp.PermissionOptionId("deny")},
		},
	})

	if err != nil {
		logger.Warn("ACPInteractionHandler: permission request failed: %v", err)
		return nil, err
	}

	if permResp.Outcome.Cancelled != nil {
		logger.Debug("ACPInteractionHandler: permission cancelled by client")
		return &actor.UserInteractionResponse{
			RequestID:    req.RequestID,
			Cancelled:    true,
			Acknowledged: true,
		}, nil
	}

	if permResp.Outcome.Selected == nil {
		logger.Debug("ACPInteractionHandler: no option selected")
		return &actor.UserInteractionResponse{
			RequestID:    req.RequestID,
			Approved:     false,
			Acknowledged: true,
			Error:        fmt.Errorf("no authorization option selected"),
		}, nil
	}

	approved := string(permResp.Outcome.Selected.OptionId) == "allow"
	logger.Debug("ACPInteractionHandler: tool=%s authorization=%v", payload.ToolName, approved)

	return &actor.UserInteractionResponse{
		RequestID:    req.RequestID,
		Approved:     approved,
		Acknowledged: true,
	}, nil
}

// getToolKind determines the appropriate tool kind based on tool name
func (h *ACPInteractionHandler) getToolKind(toolName string, parameters map[string]interface{}) acp.ToolKind {
	switch toolName {
	case "read_file", "read_file_summarized":
		return acp.ToolKindRead
	case "create_file", "edit_file", "write_file_replace":
		return acp.ToolKindEdit
	case "shell", "go_sandbox", "command":
		return acp.ToolKindExecute
	case "search_file_content", "search_files", "web_search":
		return acp.ToolKindSearch
	default:
		return acp.ToolKindEdit // Default fallback
	}
}

// extractLocations extracts file locations from tool parameters
func (h *ACPInteractionHandler) extractLocations(toolName string, parameters map[string]interface{}) []acp.ToolCallLocation {
	var locations []acp.ToolCallLocation

	switch toolName {
	case "read_file", "create_file", "edit_file", "write_file_replace":
		if path, ok := parameters["path"].(string); ok {
			locations = append(locations, acp.ToolCallLocation{
				Path: path,
			})
		}
	case "search_file_content", "search_files":
		if path, ok := parameters["path"].(string); ok {
			locations = append(locations, acp.ToolCallLocation{
				Path: path,
			})
		}
	}

	return locations
}
