package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// sessionStartHook is the JSON shape Claude Code reads from a SessionStart
// hook's stdout. SystemMessage is displayed to the USER in the terminal;
// HookSpecificOutput.AdditionalContext is injected into the model's context
// (wrapped in a system reminder). Emitting both makes a nudge visible to the
// human AND available to the assistant from one hook run.
type sessionStartHook struct {
	HookSpecificOutput sessionStartPayload `json:"hookSpecificOutput"`
	SystemMessage      string              `json:"systemMessage"`
}

type sessionStartPayload struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// writeSessionStartHook emits the SessionStart hook JSON to w. userMsg becomes
// the user-visible systemMessage; modelCtx becomes the additionalContext fed to
// the model. Callers with nothing to say must NOT call it — an empty hook output
// stays empty, so Claude Code shows no spurious banner for "no nudge".
//
// HTML escaping is disabled so text like "<file-or-symbol>" survives verbatim
// instead of becoming <… noise in the emitted JSON.
func writeSessionStartHook(w io.Writer, userMsg, modelCtx string) error {
	out := sessionStartHook{
		HookSpecificOutput: sessionStartPayload{
			HookEventName:     "SessionStart",
			AdditionalContext: modelCtx,
		},
		SystemMessage: userMsg,
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("writeSessionStartHook: %w", err)
	}
	return nil
}
