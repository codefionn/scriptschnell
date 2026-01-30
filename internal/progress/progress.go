package progress

import "strings"

type ReportMode int

const (
	// ReportNoStatus streams only (no status indicator).
	ReportNoStatus ReportMode = iota
	// ReportJustStatus reports to status indicator only.
	ReportJustStatus
	// ReportStreamAndStatus reports to both stream and status indicator.
	ReportStreamAndStatus
)

// Update describes a progress or streaming message emitted by the orchestrator or tools.
type Update struct {
	// Message is the content to deliver to the UI or client.
	Message string
	// Reasoning is the reasoning/thinking content (for extended thinking models).
	Reasoning string
	// AddNewLine appends a newline to Message if one is not already present.
	AddNewLine bool
	// Mode controls where the message should be surfaced.
	Mode ReportMode
	// Ephemeral marks the update as transient (should not persist once superseded).
	Ephemeral bool
}

// ShouldStream returns true if the update should be streamed to the user-facing content channel.
func (u Update) ShouldStream() bool {
	return u.Mode == ReportNoStatus || u.Mode == ReportStreamAndStatus
}

// ShouldStatus returns true if the update should be shown in a status indicator.
func (u Update) ShouldStatus() bool {
	return u.Mode == ReportJustStatus || u.Mode == ReportStreamAndStatus
}

// Callback receives progress updates.
type Callback func(Update) error

// Normalize ensures the update reflects requested formatting (currently newline handling).
func Normalize(update Update) Update {
	if update.AddNewLine && update.Message != "" && !strings.HasSuffix(update.Message, "\n") {
		update.Message += "\n"
	}
	return update
}

// Dispatch normalizes and sends the update if the callback is set.
func Dispatch(cb Callback, update Update) error {
	if cb == nil {
		return nil
	}
	return cb(Normalize(update))
}
