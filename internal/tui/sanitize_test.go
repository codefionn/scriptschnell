package tui

import "testing"

func TestSanitizePromptInput(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no escape sequences",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "osc bel terminator",
			in:   "\x1b]11;rgb:1818/1818/1818\x07Hello",
			want: "Hello",
		},
		{
			name: "osc st terminator",
			in:   "Ask\x1b]11;rgb:1818/1818/1818\x1b\\ me",
			want: "Ask me",
		},
		{
			name: "csi color sequence",
			in:   "\x1b[31mred\x1b[0m text",
			want: "red text",
		},
		{
			name: "osc without esc prefix",
			in:   "]11;rgb:1818/1818/1818\x07",
			want: "",
		},
		{
			name: "non osc bracket sequence",
			in:   "prefix ]not-osc",
			want: "prefix ]not-osc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var state ansiSanitizeState
			got := sanitizePromptInput(tt.in, &state)
			if got != tt.want {
				t.Fatalf("sanitizePromptInput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSanitizePromptInputFragmentedOSC(t *testing.T) {
	var state ansiSanitizeState
	if got := sanitizePromptInput("\x1b", &state); got != "" {
		t.Fatalf("expected empty string after ESC, got %q", got)
	}
	if !state.pendingESC {
		t.Fatalf("expected pending ESC state to be true")
	}
	if state.escContext != ansiModeNone {
		t.Fatalf("expected pending ESC context none, got %v", state.escContext)
	}
	if got := sanitizePromptInput("]11;rgb:1818/1818/1818", &state); got != "" {
		t.Fatalf("expected OSC payload to be stripped, got %q", got)
	}
	if state.mode != ansiModeNone {
		t.Fatalf("expected OSC detection to reset when unterminated, got %v", state.mode)
	}
	if got := sanitizePromptInput("\x07", &state); got != "" {
		t.Fatalf("expected BEL terminator to be consumed, got %q", got)
	}
	if state.mode != ansiModeNone {
		t.Fatalf("expected mode to reset, got %v", state.mode)
	}
	if state.pendingESC {
		t.Fatalf("expected pending ESC to be cleared")
	}
}

func TestSanitizePromptInputFragmentedOSCST(t *testing.T) {
	var state ansiSanitizeState
	steps := []string{"\x1b", "]11;rgb:1818/1818/1818", "\x1b", "\\"}
	for _, chunk := range steps {
		got := sanitizePromptInput(chunk, &state)
		if got != "" {
			t.Fatalf("expected chunk %q to be sanitized to empty, got %q", chunk, got)
		}
	}
	if state.mode != ansiModeNone {
		t.Fatalf("expected mode reset after ST terminator, got %v", state.mode)
	}
	if state.pendingESC {
		t.Fatalf("expected no pending ESC after ST terminator")
	}
}

func TestStripANSISequences(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "no escape sequences",
			in:   "hello world",
			want: "hello world",
		},
		{
			name: "csi color sequence",
			in:   "\x1b[31mred\x1b[0m text",
			want: "red text",
		},
		{
			name: "csi cursor movement",
			in:   "\x1b[2K\x1b[1Gprogress",
			want: "progress",
		},
		{
			name: "osc bel terminator",
			in:   "\x1b]11;rgb:1818/1818/1818\x07Hello",
			want: "Hello",
		},
		{
			name: "osc st terminator",
			in:   "Ask\x1b]11;rgb:1818/1818/1818\x1b\\ me",
			want: "Ask me",
		},
		{
			name: "dcs sequence",
			in:   "\x1bP@\x1b\\output",
			want: "output",
		},
		{
			name: "multiple mixed sequences",
			in:   "\x1b[31mred\x1b[0m and \x1b[1mbold\x1b[0m text\x1b[K",
			want: "red and bold text",
		},
		{
			name: "trailing esc",
			in:   "text\x1b",
			want: "text",
		},
		{
			name: "empty string",
			in:   "",
			want: "",
		},
		{
			name: "realistic ls output with colors",
			in:   "\x1b[0m\x1b[01;34mdir\x1b[0m\n\x1b[01;32mfile.txt\x1b[0m",
			want: "dir\nfile.txt",
		},
		{
			name: "escapes in middle of text",
			in:   "before \x1b[31mcolored\x1b[0m after",
			want: "before colored after",
		},
		{
			name: "csi with multiple parameters",
			in:   "\x1b[38;5;208mtext\x1b[0m",
			want: "text",
		},
		{
			name: "progress bar with esc",
			in:   "\r\x1b[0K[=====>     ]\r\x1b[0K[======>    ]",
			want: "[=====>     ][======>    ]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripANSISequences(tt.in)
			if got != tt.want {
				t.Fatalf("stripANSISequences() = %q, want %q", got, tt.want)
			}
		})
	}
}
