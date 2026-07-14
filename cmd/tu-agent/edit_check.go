package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// editCheckCmd is the PostToolUse (Write|Edit) hook target for the
// edit-without-context behavioral signal: it flags an edit made without a
// preceding graph context query (get_context/get_impact/find_symbol/
// get_concept) earlier in the same session transcript.
var editCheckCmd = &cobra.Command{
	Use:    "edit-check",
	Short:  "PostToolUse hook: flag an edit made without a preceding graph context query",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		return editCheckDecision(cmd.InOrStdin())
	},
}

// contextToolNames are the graph query tool names as they appear inside MCP
// tool_use names in the transcript (e.g. mcp__plugin_tu-agent_tu-agent-graph__
// get_context). Any transcript line mentioning one of these AND the edited
// file's basename counts as "context was queried".
var contextToolNames = []string{"get_context", "get_impact", "find_symbol", "get_concept"}

// editCheckDecision reads a PostToolUse hook payload from r and, at full
// telemetry level only, records an edit-without-context violation when the
// edited file's transcript shows no preceding graph context query. Never
// fails the hook: any read/parse problem is a silent no-op.
func editCheckDecision(r io.Reader) error {
	if telemetryLevel() != "full" {
		return nil
	}
	var payload struct {
		SessionID      string `json:"session_id"`
		TranscriptPath string `json:"transcript_path"`
		ToolInput      struct {
			FilePath string `json:"file_path"`
		} `json:"tool_input"`
	}
	if err := json.NewDecoder(r).Decode(&payload); err != nil {
		return nil
	}
	if payload.ToolInput.FilePath == "" {
		return nil
	}
	if editWithoutContext(payload.TranscriptPath, payload.ToolInput.FilePath) {
		recordViolation("edit-without-context", payload.ToolInput.FilePath, payload.SessionID)
	}
	return nil
}

// transcriptLine is one JSONL message object. Tool blocks live under
// message.content, which is an array of heterogeneous blocks.
type transcriptLine struct {
	Message struct {
		Content []transcriptBlock `json:"content"`
	} `json:"message"`
}

// transcriptBlock is one content block. Only the fields the edit-without-context
// pairing needs are decoded; Content is kept raw because a tool_result's content
// may be a string OR an array of blocks.
type transcriptBlock struct {
	Type      string          `json:"type"`
	Name      string          `json:"name"`        // tool_use: MCP tool name
	ID        string          `json:"id"`          // tool_use: id, paired by tool_result
	ToolUseID string          `json:"tool_use_id"` // tool_result: back-reference to a tool_use id
	Content   json.RawMessage `json:"content"`     // tool_result: string or array of blocks
}

// editWithoutContext reports whether editedFile was edited without a preceding
// graph context query in the transcript at transcriptPath. A context query
// counts when EITHER:
//   - fast path / drift fallback: a single raw JSONL line mentions a
//     context-tool name AND contains the edited file's basename (covers a
//     path-valued query and any format drift), OR
//   - id pairing: a tool_use block with a context-tool name records its id,
//     and a LATER tool_result block referencing that id has the basename in its
//     raw content (covers a SYMBOL-valued query whose basename only surfaces in
//     the tool_result on a separate line).
//
// Lenient by design (few false violations). Fail-open: a missing/unreadable
// transcript or a scanner error yields false (never flag on missing data).
func editWithoutContext(transcriptPath, editedFile string) bool {
	if transcriptPath == "" {
		return false
	}
	f, err := os.Open(transcriptPath)
	if err != nil {
		return false
	}
	defer f.Close()

	base := filepath.Base(editedFile)
	baseBytes := []byte(base)
	contextIDs := make(map[string]bool)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		raw := sc.Bytes()

		// Fast path / drift fallback: a context-tool name and the basename on
		// the same raw line (path-valued query).
		if lineMentionsContextTool(string(raw)) && bytes.Contains(raw, baseBytes) {
			return false
		}

		// id pairing: tolerate non-JSON / non-message lines by skipping them.
		var line transcriptLine
		if err := json.Unmarshal(raw, &line); err != nil {
			continue
		}
		for _, b := range line.Message.Content {
			switch b.Type {
			case "tool_use":
				if b.ID != "" && lineMentionsContextTool(b.Name) {
					contextIDs[b.ID] = true
				}
			case "tool_result":
				if contextIDs[b.ToolUseID] && bytes.Contains(b.Content, baseBytes) {
					return false
				}
			}
		}
	}
	return sc.Err() == nil
}

// lineMentionsContextTool reports whether line references one of the graph
// query tool names.
func lineMentionsContextTool(line string) bool {
	for _, name := range contextToolNames {
		if strings.Contains(line, name) {
			return true
		}
	}
	return false
}

func init() {
	hookCmd.AddCommand(editCheckCmd)
}
