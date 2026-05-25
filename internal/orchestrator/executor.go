package orchestrator

import (
	"encoding/json"
	"fmt"
	"time"
)

// Executor uses Haiku to execute sub-tasks from a plan.
type Executor struct {
	client     *LLMClient
	compressor CompressFunc
	config     Config
}

func NewExecutor(client *LLMClient, compressor CompressFunc, config Config) *Executor {
	return &Executor{client: client, compressor: compressor, config: config}
}

// ExecuteSubTask runs one sub-task using Haiku.
func (e *Executor) ExecuteSubTask(subTask SubTask, taskContext string, previousResults []SubTaskResult) (*SubTaskResult, error) {
	start := time.Now()

	if e.client == nil {
		return &SubTaskResult{
			SubTaskID:    subTask.ID,
			Success:      false,
			ErrorMessage: "no LLM client configured",
			LatencyMs:    int(time.Since(start).Milliseconds()),
			Model:        Haiku,
		}, nil
	}

	specJSON, _ := json.Marshal(subTask.Spec)

	system := "Execute this task exactly as specified. Output ONLY the result — no explanation, no markdown header."
	userMsg := fmt.Sprintf("Sub-task ID: %s\nAction: %s\nSpec:\n%s", subTask.ID, subTask.Action, string(specJSON))

	if taskContext != "" {
		userMsg = "Original request: " + taskContext + "\n\n" + userMsg
	}

	if len(previousResults) > 0 {
		context := "\n\nContext from previous steps:\n"
		for _, pr := range previousResults {
			if pr.Success {
				context += fmt.Sprintf("[%s]: %s\n", pr.SubTaskID, truncate(pr.Output, 300))
			}
		}
		userMsg += context
	}

	if e.compressor != nil {
		userMsg = e.compressor(userMsg, "", "")
	}

	resp, err := e.client.Call(Haiku, system, userMsg, e.config.HaikuConfig.MaxTokens)
	if err != nil {
		return &SubTaskResult{
			SubTaskID:    subTask.ID,
			Success:      false,
			ErrorMessage: err.Error(),
			LatencyMs:    int(time.Since(start).Milliseconds()),
			Model:        Haiku,
		}, nil
	}

	return &SubTaskResult{
		SubTaskID:  subTask.ID,
		Success:    true,
		Output:     resp.Content,
		TokensUsed: resp.InputTokens + resp.OutputTokens,
		LatencyMs:  int(time.Since(start).Milliseconds()),
		Model:      Haiku,
	}, nil
}

// ExecuteWithSkill runs a task using a skill recipe (ModeSkillExecute).
func (e *Executor) ExecuteWithSkill(task Task, skillInstruction string) (*SubTaskResult, error) {
	start := time.Now()

	userMsg := task.Prompt
	if e.compressor != nil {
		userMsg = e.compressor(task.Prompt, "", "")
	}

	resp, err := e.client.Call(Haiku, skillInstruction, userMsg, e.config.HaikuConfig.MaxTokens)
	if err != nil {
		return &SubTaskResult{
			SubTaskID:    task.ID,
			Success:      false,
			ErrorMessage: err.Error(),
			LatencyMs:    int(time.Since(start).Milliseconds()),
			Model:        Haiku,
		}, nil
	}

	return &SubTaskResult{
		SubTaskID:  task.ID,
		Success:    true,
		Output:     resp.Content,
		TokensUsed: resp.InputTokens + resp.OutputTokens,
		LatencyMs:  int(time.Since(start).Milliseconds()),
		Model:      Haiku,
	}, nil
}

// ExecuteWithMemory applies a known solution from memory (ModeMemoryApply).
func (e *Executor) ExecuteWithMemory(task Task, memorySummary string) (*SubTaskResult, error) {
	start := time.Now()

	system := "You are applying a known solution from memory. Follow the memory context carefully."
	userMsg := fmt.Sprintf("Memory context:\n%s\n\nTask:\n%s", memorySummary, task.Prompt)

	if e.compressor != nil {
		userMsg = e.compressor(userMsg, "", "")
	}

	resp, err := e.client.Call(Haiku, system, userMsg, e.config.HaikuConfig.MaxTokens)
	if err != nil {
		return &SubTaskResult{
			SubTaskID:    task.ID,
			Success:      false,
			ErrorMessage: err.Error(),
			LatencyMs:    int(time.Since(start).Milliseconds()),
			Model:        Haiku,
		}, nil
	}

	return &SubTaskResult{
		SubTaskID:  task.ID,
		Success:    true,
		Output:     resp.Content,
		TokensUsed: resp.InputTokens + resp.OutputTokens,
		LatencyMs:  int(time.Since(start).Milliseconds()),
		Model:      Haiku,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
