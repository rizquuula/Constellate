package terminal

import "testing"

func TestComputeActivity(t *testing.T) {
	const window int64 = 2
	now := int64(1000)

	tests := []struct {
		name        string
		lastOutput  int64
		prompt      PromptState
		isQuestion  bool
		wantActivity Activity
	}{
		// Active window: output arrived recently → always Active.
		{
			name: "active window - recent output",
			lastOutput: now - 1, // 1s ago, within 2s window
			prompt:     PromptUnknown,
			isQuestion: false,
			wantActivity: ActivityActive,
		},
		{
			name: "active window - at boundary",
			lastOutput: now - 1,
			prompt:     PromptAtPrompt,
			isQuestion: true,
			wantActivity: ActivityActive, // recent output beats everything
		},

		// Output stalled + PromptRunning.
		{
			name: "running + question → awaiting-input",
			lastOutput: now - 10,
			prompt:     PromptRunning,
			isQuestion: true,
			wantActivity: ActivityAwaitingInput,
		},
		{
			name: "running + quiet → active (task in progress)",
			lastOutput: now - 10,
			prompt:     PromptRunning,
			isQuestion: false,
			wantActivity: ActivityActive,
		},

		// Output stalled + PromptAtPrompt.
		{
			name: "at-prompt → idle",
			lastOutput: now - 10,
			prompt:     PromptAtPrompt,
			isQuestion: false,
			wantActivity: ActivityIdle,
		},
		{
			name: "at-prompt + question → idle (prompt state wins)",
			lastOutput: now - 10,
			prompt:     PromptAtPrompt,
			isQuestion: true,
			wantActivity: ActivityIdle,
		},

		// Output stalled + PromptUnknown.
		{
			name: "unknown + question → awaiting-input",
			lastOutput: now - 10,
			prompt:     PromptUnknown,
			isQuestion: true,
			wantActivity: ActivityAwaitingInput,
		},
		{
			name: "unknown + quiet + has output → idle",
			lastOutput: now - 10,
			prompt:     PromptUnknown,
			isQuestion: false,
			wantActivity: ActivityIdle,
		},

		// Zero lastOutputAt + PromptUnknown → Unknown.
		{
			name: "zero lastOutput + unknown → unknown",
			lastOutput: 0,
			prompt:     PromptUnknown,
			isQuestion: false,
			wantActivity: ActivityUnknown,
		},
		{
			name: "zero lastOutput + unknown + question → unknown",
			lastOutput: 0,
			prompt:     PromptUnknown,
			isQuestion: true,
			wantActivity: ActivityUnknown,
		},
		// Zero lastOutput + PromptAtPrompt → Idle (shell marker overrides).
		{
			name: "zero lastOutput + at-prompt → idle",
			lastOutput: 0,
			prompt:     PromptAtPrompt,
			isQuestion: false,
			wantActivity: ActivityIdle,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ComputeActivity(now, tc.lastOutput, window, tc.prompt, tc.isQuestion)
			if got != tc.wantActivity {
				t.Errorf("ComputeActivity(%v, %v, %v, %v, %v) = %q, want %q",
					now, tc.lastOutput, window, tc.prompt, tc.isQuestion, got, tc.wantActivity)
			}
		})
	}
}

func TestTailLooksLikeQuestion(t *testing.T) {
	tests := []struct {
		line string
		want bool
	}{
		// Positive cases.
		{"Do you want to continue?", true},
		{"Continue? ", true},            // trailing space trimmed
		{"Overwrite file.txt? ", true},
		{"proceed?", true},
		{"Are you sure (y/n)", true},
		{"[Y/N] ", true},
		{"(yes/no)", true},
		{"Enter password:", true},
		{"Enter passphrase:", true},
		{"Press Enter to continue", true},
		{"Press any key to quit", true},
		{"❯ ", true},
		{"Do you want this? (y/n)", true},
		{"OVERWRITE? ", true},           // case-insensitive
		{"Password: ", true},

		// Negative cases.
		{"", false},
		{"   ", false},
		{"hello world", false},
		{"command completed successfully", false},
		{"error: file not found", false},
		{"100%", false},
		{"make[1]: Leaving directory", false},
		// Long line ending in "?" must NOT be treated as a prompt (log/prose guard).
		{"This is a very long prose line that happens to end with a question mark and should not trigger the heuristic?", false},
		// Short line ending in "?" IS a prompt.
		{"Overwrite file?", true},
	}

	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			got := TailLooksLikeQuestion(tc.line)
			if got != tc.want {
				t.Errorf("TailLooksLikeQuestion(%q) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}
