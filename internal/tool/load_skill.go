package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/carlosneir4/tu-agent/internal/skill"
	"github.com/carlosneir4/tu-agent/internal/telemetry"
)

const loadSkillInputSchema = `{
    "type": "object",
    "properties": {
        "name": {
            "type": "string",
            "description": "The name of the skill to load (as listed in the skill index)."
        }
    },
    "required": ["name"]
}`

// LoadSkillTool fetches full SKILL.md content by name from the skill index.
type LoadSkillTool struct {
	index *skill.Index
	tel   *telemetry.Logger
}

var _ Tool = (*LoadSkillTool)(nil)

// NewLoadSkillTool returns a new LoadSkillTool backed by idx.
// tel may be nil; in that case telemetry logging is skipped.
func NewLoadSkillTool(idx *skill.Index, tel *telemetry.Logger) *LoadSkillTool {
	return &LoadSkillTool{index: idx, tel: tel}
}

func (t *LoadSkillTool) Name() string { return "load_skill" }
func (t *LoadSkillTool) Description() string {
	return "Load the full content of a skill by name. " +
		"Use the skill index in the system prompt to find available skill names."
}
func (t *LoadSkillTool) InputSchema() json.RawMessage {
	return json.RawMessage(loadSkillInputSchema)
}

type loadSkillInput struct {
	Name string `json:"name"`
}

// Run loads and returns the full SKILL.md content for the named skill.
func (t *LoadSkillTool) Run(_ context.Context, input json.RawMessage) (string, error) {
	var in loadSkillInput
	if err := json.Unmarshal(input, &in); err != nil {
		return "", fmt.Errorf("load_skill.Run: parsing input: %w", err)
	}
	if in.Name == "" {
		return "", fmt.Errorf("load_skill.Run: name is empty")
	}
	content, err := t.index.LoadContent(in.Name)
	if t.tel != nil {
		if lerr := t.tel.Log(telemetry.Entry{
			Timestamp: time.Now().UTC(),
			Event:     "load_skill",
			Skill:     in.Name,
			Found:     err == nil,
		}); lerr != nil {
			slog.Warn("load_skill: telemetry", "err", lerr)
		}
	}
	if err != nil {
		return "", fmt.Errorf("load_skill.Run: %w", err)
	}
	return content, nil
}
