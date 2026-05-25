package skills

import (
	"fmt"
	"strings"
	"time"
)

// Executor applies a skill by injecting it into the agent prompt.
type Executor struct {
	store  *Store
	config Config
}

// NewExecutor creates a new Executor.
func NewExecutor(store *Store, config Config) *Executor {
	return &Executor{store: store, config: config}
}

// Apply applies a skill to a task, returning the assembled system prompt.
func (e *Executor) Apply(skill *Skill, taskPrompt string, graphContext string) (string, error) {
	if skill == nil {
		return "", fmt.Errorf("nil skill")
	}

	var b strings.Builder

	b.WriteString("SKILL INSTRUCTION (follow this recipe):\n")
	b.WriteString(skill.Instruction)
	b.WriteString("\n\n")

	if graphContext != "" {
		b.WriteString("GRAPH CONTEXT:\n")
		b.WriteString(graphContext)
		b.WriteString("\n\n")
	}

	successRate := 0.0
	if skill.TimesApplied > 0 {
		successRate = float64(skill.TimesSucceeded) / float64(skill.TimesApplied) * 100
	}
	b.WriteString(fmt.Sprintf("NOTE: This skill has been applied %d times with %.0f%% success rate.\n",
		skill.TimesApplied, successRate))

	// Warn if stale
	for _, tag := range skill.NegativeTags {
		if tag.Context == "graph_changed" {
			b.WriteString("WARNING: The code this skill covers may have changed recently. Verify before applying blindly.\n")
			break
		}
	}

	b.WriteString("\nTASK:\n")
	b.WriteString(taskPrompt)

	return b.String(), nil
}

// RecordExecution logs the result of applying a skill and updates metrics.
func (e *Executor) RecordExecution(skillID string, execution SkillExecution) error {
	execution.SkillID = skillID
	if _, err := e.store.InsertExecution(execution); err != nil {
		return fmt.Errorf("insert execution: %w", err)
	}

	tokensSaved := 0.0
	if execution.TokensUsed > 0 {
		// Rough estimate: skill application saves ~40% of what free-reasoning would use
		tokensSaved = float64(execution.TokensUsed) * 0.4
	}

	if err := e.store.UpdateMetrics(skillID, execution.Success, execution.Complexity, tokensSaved); err != nil {
		return fmt.Errorf("update metrics: %w", err)
	}

	// Add negative tag on context-specific failure
	if !execution.Success && execution.ErrorDetail != "" {
		tag := inferNegativeTag(execution)
		if tag != nil {
			_ = e.store.AddNegativeTag(skillID, *tag)
		}
	}

	return nil
}

// RecordSessionWithoutSkill logs a successful task where no skill matched.
func (e *Executor) RecordSessionWithoutSkill(execution SkillExecution) error {
	execution.SkillID = ""
	_, err := e.store.InsertExecution(execution)
	return err
}

func inferNegativeTag(exec SkillExecution) *NegativeTag {
	lower := strings.ToLower(exec.ErrorDetail)

	if strings.Contains(lower, "python") || strings.Contains(lower, ".py") {
		return &NegativeTag{
			Context: "python repo",
			Reason:  "skill failed in Python context: " + truncate(exec.ErrorDetail, 100),
			AddedAt: time.Now().UTC().Format(time.RFC3339),
		}
	}
	if strings.Contains(lower, "cross-repo") || strings.Contains(lower, "cross repo") {
		return &NegativeTag{
			Context: "cross-repo with many nodes",
			Reason:  "skill failed in cross-repo context: " + truncate(exec.ErrorDetail, 100),
			AddedAt: time.Now().UTC().Format(time.RFC3339),
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
