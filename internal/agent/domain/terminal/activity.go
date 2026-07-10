package terminal

import (
	"strings"
	"unicode"
)

// Activity represents the inferred activity state of a terminal session.
type Activity string

const (
	ActivityActive        Activity = "active"
	ActivityIdle          Activity = "idle"
	ActivityAwaitingInput Activity = "awaiting-input"
	ActivityUnknown       Activity = "unknown"
)

// PromptState is derived from OSC 133 shell-integration markers (FinalTerm /
// iTerm2 / VS Code prompt sequences). It lets ComputeActivity distinguish a
// quiet command run from a shell sitting at a prompt.
type PromptState int

const (
	// PromptUnknown means no shell-integration markers have been seen.
	PromptUnknown PromptState = iota
	// PromptAtPrompt means the shell is waiting for the user's next command
	// (OSC 133;A "prompt start", 133;B "command-line start", or 133;D "command done").
	PromptAtPrompt
	// PromptRunning means a command is executing (OSC 133;C "pre-execution").
	PromptRunning
)

// ComputeActivity derives a session's activity from observable signals.
//
// activeWindow is how recently (in seconds) output must have arrived to count
// as "active". The threshold is intentionally short (callers use 2 s) so tiles
// flip to idle quickly after a command finishes.
//
// Logic (in order):
//  1. Output flowing recently → Active (overrides everything).
//  2. Output stalled + PromptRunning + tail looks like a question → AwaitingInput.
//  3. Output stalled + PromptRunning + no question → Active (quiet task in progress).
//  4. Output stalled + PromptAtPrompt → Idle (shell waiting for input).
//  5. Output stalled + PromptUnknown + tail looks like a question → AwaitingInput.
//  6. Output stalled + PromptUnknown + no question → Idle.
//  7. lastOutputAt == 0 and PromptUnknown → Unknown (no data yet).
func ComputeActivity(now, lastOutputAt int64, activeWindowSec int64, prompt PromptState, tailLooksLikeQuestion bool) Activity {
	outputFlowing := lastOutputAt > 0 && now-lastOutputAt < activeWindowSec
	if outputFlowing {
		return ActivityActive
	}

	// No recent output.
	switch prompt {
	case PromptRunning:
		if tailLooksLikeQuestion {
			return ActivityAwaitingInput
		}
		return ActivityActive

	case PromptAtPrompt:
		return ActivityIdle

	default: // PromptUnknown
		if lastOutputAt == 0 {
			return ActivityUnknown
		}
		if tailLooksLikeQuestion {
			return ActivityAwaitingInput
		}
		return ActivityIdle
	}
}

// SessionActivity pairs a session ID with its computed activity and its live
// working directory. It mirrors SessionRev for the same lightweight-list pattern.
type SessionActivity struct {
	ID       string
	Activity Activity
	Pwd      string
}

// trailingQuestionMaxRunes is the maximum rune length a line may have for a
// bare trailing "?" to be treated as an interactive prompt. Long lines ending in
// "?" are log/prose output rather than prompts.
const trailingQuestionMaxRunes = 60

// questionSubstrings are substrings anywhere in the trimmed line that reliably
// indicate an interactive prompt. Checked case-insensitively.
var questionSubstrings = []string{
	"(y/n)",
	"[y/n]",
	"(yes/no)",
	"password:",
	"passphrase:",
	"continue?",
	"proceed?",
	"overwrite?",
	"do you want",
	"press enter",
	"press any key",
	"❯",
}

// TailLooksLikeQuestion reports whether line (the tail text from a terminal
// screen) looks like an interactive prompt waiting for the user's answer.
// It is case-insensitive and operates on the trimmed input.
func TailLooksLikeQuestion(line string) bool {
	trimmed := strings.TrimRightFunc(line, unicode.IsSpace)
	if trimmed == "" {
		return false
	}
	lower := strings.ToLower(trimmed)

	for _, sub := range questionSubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}

	// A bare trailing "?" is only treated as a prompt on short lines to avoid
	// false-positives from log/prose lines that happen to end in "?".
	if strings.HasSuffix(lower, "?") && len([]rune(trimmed)) <= trailingQuestionMaxRunes {
		return true
	}

	return false
}
