package orchestrator

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Orchestrator is the main entry point for Engine 5.
type Orchestrator struct {
	router   *Router
	planner  *Planner
	executor *Executor
	verifier *Verifier
	esc      *Escalation
	parallel *ParallelExecutor
	tracker  *Tracker
	client   *LLMClient
	config   Config
}

// NewOrchestrator creates a fully wired orchestrator.
func NewOrchestrator(
	skills SkillMatcher,
	memory MemoryRecaller,
	graph GraphAnalyzer,
	compressor CompressFunc,
	config Config,
) (*Orchestrator, error) {
	client := NewLLMClient(config.OpusConfig, config.HaikuConfig)
	router := NewRouter(skills, memory, graph, config)
	planner := NewPlanner(client, compressor, config)
	executor := NewExecutor(client, compressor, config)
	verifier := NewVerifier(client, config)
	esc := NewEscalation(executor, planner, client, config)
	parallel := NewParallelExecutor(executor, esc, config)

	var tracker *Tracker
	var err error
	if config.DatabaseURL != "" {
		tracker, err = NewTracker(config.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("tracker: %w", err)
		}
	} else {
		tracker, _ = NewTracker("")
	}

	return &Orchestrator{
		router:   router,
		planner:  planner,
		executor: executor,
		verifier: verifier,
		esc:      esc,
		parallel: parallel,
		tracker:  tracker,
		client:   client,
		config:   config,
	}, nil
}

// Execute handles a developer's request end-to-end.
func (o *Orchestrator) Execute(task Task) (*TaskResult, error) {
	if task.ID == "" {
		task.ID = uuid.NewString()
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = time.Now()
	}
	start := time.Now()

	// STEP 1: CLASSIFY
	if task.TaskType == "" {
		task.TaskType = ClassifyTaskType(task.Prompt)
	}

	// STEP 2: ROUTE
	decision, err := o.router.Route(task)
	if err != nil {
		return nil, fmt.Errorf("routing: %w", err)
	}
	log.Printf("orchestrator: task %s routed to %s — %s", task.ID, decision.Mode, decision.Reason)

	var taskResult *TaskResult

	switch decision.Mode {

	case ModeSkillExecute:
		taskResult, err = o.executeWithSkill(task, decision)

	case ModeMemoryApply:
		taskResult, err = o.executeWithMemory(task, decision)

	case ModePlanExecute, ModeFullOrchestration:
		taskResult, err = o.executePlanBased(task, decision)

	case ModeSingleOpus:
		taskResult, err = o.executeSingle(task, decision, Opus)

	case ModeSingleHaiku:
		taskResult, err = o.executeSingle(task, decision, Haiku)

	default:
		taskResult, err = o.executeSingle(task, decision, Opus)
	}

	if err != nil {
		return nil, err
	}

	taskResult.TaskID = task.ID
	taskResult.RoutingMode = decision.Mode
	taskResult.MemoryHit = decision.MemoryHit
	taskResult.LatencyMs = int(time.Since(start).Milliseconds())

	// STEP 4: TRACK (async)
	go o.tracker.LogTask(task.ID, decision, taskResult.SubResults, taskResult.Escalations)

	return taskResult, nil
}

func (o *Orchestrator) executeWithSkill(task Task, decision *RoutingDecision) (*TaskResult, error) {
	result, err := o.executor.ExecuteWithSkill(task, decision.MatchedSkillID)
	if err != nil {
		return nil, err
	}

	// Tier 1 verify (zero Opus tokens)
	dummy := SubTask{ID: task.ID, Action: "modify_file", VerifyTier: VerifyAutomated}
	vr, _ := o.verifier.Verify(dummy, result, VerifyAutomated)

	return &TaskResult{
		Success:     result.Success && (vr == nil || vr.Passed),
		Output:      result.Output,
		TotalCost:   calculateCost(result.Model, result.TokensUsed/2, result.TokensUsed/2),
		TotalTokens: result.TokensUsed,
		SubResults:  []SubTaskResult{*result},
		SkillUsed:   decision.MatchedSkillID,
	}, nil
}

func (o *Orchestrator) executeWithMemory(task Task, decision *RoutingDecision) (*TaskResult, error) {
	result, err := o.executor.ExecuteWithMemory(task, "memory context")
	if err != nil {
		return nil, err
	}

	dummy := SubTask{ID: task.ID, Action: "modify_file", VerifyTier: VerifySpotCheck}
	o.verifier.Verify(dummy, result, VerifySpotCheck) //nolint:errcheck

	return &TaskResult{
		Success:     result.Success,
		Output:      result.Output,
		TotalCost:   calculateCost(result.Model, result.TokensUsed/2, result.TokensUsed/2),
		TotalTokens: result.TokensUsed,
		SubResults:  []SubTaskResult{*result},
	}, nil
}

func (o *Orchestrator) executePlanBased(task Task, decision *RoutingDecision) (*TaskResult, error) {
	plan, err := o.planner.CreatePlan(task, decision)
	if err != nil {
		return nil, fmt.Errorf("create plan: %w", err)
	}

	results, escalations, err := o.parallel.ExecuteAll(plan, task.Prompt)
	if err != nil {
		return nil, fmt.Errorf("execute plan: %w", err)
	}

	// batch verify
	var items []VerifyItem
	for i, st := range plan.SubTasks {
		r := results[i]
		items = append(items, VerifyItem{SubTask: st, Result: &r, Tier: st.VerifyTier})
	}
	verifyResults, _ := o.verifier.VerifyBatch(items)

	totalTokens := 0
	totalCost := 0.0
	allPassed := true
	var outputs []string

	for i, r := range results {
		totalTokens += r.TokensUsed
		totalCost += calculateCost(r.Model, r.TokensUsed/2, r.TokensUsed/2)
		if r.Success {
			outputs = append(outputs, r.Output)
		}
		if i < len(verifyResults) && !verifyResults[i].Passed {
			allPassed = false
		}
	}

	return &TaskResult{
		Success:     allPassed,
		Output:      strings.Join(outputs, "\n\n"),
		TotalCost:   totalCost,
		TotalTokens: totalTokens,
		SubResults:  results,
		Escalations: escalations,
	}, nil
}

func (o *Orchestrator) executeSingle(task Task, decision *RoutingDecision, model ModelTier) (*TaskResult, error) {
	system := "You are a skilled software engineer. Complete the following task accurately and concisely."
	userMsg := task.Prompt

	resp, err := o.client.Call(model, system, userMsg, o.config.OpusConfig.MaxTokens)
	if err != nil {
		return nil, err
	}

	result := &SubTaskResult{
		SubTaskID:  task.ID,
		Success:    true,
		Output:     resp.Content,
		TokensUsed: resp.InputTokens + resp.OutputTokens,
		LatencyMs:  resp.LatencyMs,
		Model:      model,
	}

	return &TaskResult{
		Success:     true,
		Output:      resp.Content,
		TotalCost:   resp.CostUSD,
		TotalTokens: resp.InputTokens + resp.OutputTokens,
		SubResults:  []SubTaskResult{*result},
	}, nil
}

// Stop gracefully shuts down the orchestrator.
func (o *Orchestrator) Stop() {
	if o.tracker != nil {
		o.tracker.Stop()
	}
}
