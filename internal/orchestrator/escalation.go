package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"
)

// EscalationRecord documents the full escalation chain for one sub-task.
type EscalationRecord struct {
	SubTaskID        string           `json:"sub_task_id"`
	Steps            []EscalationStep `json:"steps"`
	FinalModel       ModelTier        `json:"final_model"`
	TotalExtraTokens int              `json:"total_extra_tokens"`
}

// Escalation handles failures in sub-task execution.
type Escalation struct {
	executor *Executor
	planner  *Planner
	client   *LLMClient
	config   Config
}

func NewEscalation(executor *Executor, planner *Planner, client *LLMClient, config Config) *Escalation {
	return &Escalation{
		executor: executor,
		planner:  planner,
		client:   client,
		config:   config,
	}
}

// HandleFailure attempts to recover from a failed sub-task via 3-step escalation.
func (esc *Escalation) HandleFailure(
	subTask SubTask,
	failedResult *SubTaskResult,
	taskContext string,
	previousResults []SubTaskResult,
) (*SubTaskResult, *EscalationRecord, error) {
	record := &EscalationRecord{SubTaskID: subTask.ID}
	extraTokens := 0

	// STEP 1: retry with error feedback
	step1Task := augmentSpecWithError(subTask, failedResult.ErrorMessage)
	result1, _ := esc.executor.ExecuteSubTask(step1Task, taskContext, previousResults)
	record.Steps = append(record.Steps, EscalationStep{
		Step:    1,
		Action:  "retry_with_error",
		Error:   failedResult.ErrorMessage,
		Success: result1.Success,
	})
	extraTokens += result1.TokensUsed
	if result1.Success {
		record.FinalModel = Haiku
		record.TotalExtraTokens = extraTokens
		return result1, record, nil
	}

	// STEP 2: Opus rephrase (skip if no client)
	rephrased, rephraseTokens, err := esc.opusRephrase(subTask, failedResult.ErrorMessage, result1.ErrorMessage)
	extraTokens += rephraseTokens
	if err == nil && rephrased != nil {
		result2, _ := esc.executor.ExecuteSubTask(*rephrased, taskContext, previousResults)
		record.Steps = append(record.Steps, EscalationStep{
			Step:    2,
			Action:  "opus_rephrase",
			Error:   result1.ErrorMessage,
			Success: result2.Success,
		})
		extraTokens += result2.TokensUsed
		if result2.Success {
			record.FinalModel = Haiku
			record.TotalExtraTokens = extraTokens
			return result2, record, nil
		}
	}

	// STEP 3: Opus takeover
	result3, takeoverTokens, err := esc.opusTakeover(subTask, taskContext,
		failedResult.ErrorMessage, result1.ErrorMessage)
	extraTokens += takeoverTokens
	record.Steps = append(record.Steps, EscalationStep{
		Step:    3,
		Action:  "opus_takeover",
		Error:   result1.ErrorMessage,
		Success: err == nil && result3 != nil && result3.Success,
	})

	if err != nil {
		record.FinalModel = Opus
		record.TotalExtraTokens = extraTokens
		return nil, record, fmt.Errorf("escalation exhausted: %w", err)
	}

	result3.Model = Opus
	record.FinalModel = Opus
	record.TotalExtraTokens = extraTokens
	return result3, record, nil
}

func augmentSpecWithError(subTask SubTask, errMsg string) SubTask {
	original, _ := json.Marshal(subTask.Spec)
	augmented := map[string]interface{}{
		"original_spec": json.RawMessage(original),
		"error":         "PREVIOUS ATTEMPT FAILED WITH: " + errMsg + "\nFix the error and try again.",
	}
	subTask.Spec = augmented
	return subTask
}

func (esc *Escalation) opusRephrase(subTask SubTask, err1, err2 string) (*SubTask, int, error) {
	if esc.client == nil {
		return nil, 0, fmt.Errorf("no LLM client for rephrase")
	}
	specJSON, _ := json.Marshal(subTask.Spec)
	system := "You are a task spec rewriter. Keep the same JSON structure but fix the spec to avoid the reported errors."
	userMsg := fmt.Sprintf(`This sub-task spec failed twice.

Spec:
%s

Error 1: %s
Error 2: %s

Rewrite the spec to avoid these errors. Keep the same JSON structure. Output ONLY the new spec JSON.`,
		string(specJSON), err1, err2)

	resp, err := esc.client.Call(Opus, system, userMsg, 600)
	if err != nil {
		return nil, 0, err
	}

	text := stripCodeFences(resp.Content)
	start := strings.Index(text, "{")
	if start == -1 {
		return nil, resp.InputTokens + resp.OutputTokens, fmt.Errorf("no JSON in rephrase response")
	}

	var newSpec interface{}
	if err := json.Unmarshal([]byte(text[start:]), &newSpec); err != nil {
		return nil, resp.InputTokens + resp.OutputTokens, err
	}

	rephrased := subTask
	rephrased.Spec = newSpec
	return &rephrased, resp.InputTokens + resp.OutputTokens, nil
}

func (esc *Escalation) opusTakeover(subTask SubTask, taskContext, err1, err2 string) (*SubTaskResult, int, error) {
	if esc.client == nil {
		return nil, 0, fmt.Errorf("no LLM client for takeover")
	}
	specJSON, _ := json.Marshal(subTask.Spec)
	system := "Complete this sub-task yourself. Generate the correct output directly."
	userMsg := fmt.Sprintf(`Complete this sub-task yourself.

Original task: %s
Sub-task spec: %s
Previous errors: %s / %s

Generate the correct output.`, taskContext, string(specJSON), err1, err2)

	resp, err := esc.client.Call(Opus, system, userMsg, esc.config.OpusConfig.MaxTokens)
	if err != nil {
		return nil, 0, err
	}

	return &SubTaskResult{
		SubTaskID:  subTask.ID,
		Success:    true,
		Output:     resp.Content,
		TokensUsed: resp.InputTokens + resp.OutputTokens,
		Model:      Opus,
	}, resp.InputTokens + resp.OutputTokens, nil
}
