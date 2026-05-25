package orchestrator

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CompressFunc is an adapter to Engine 4's BuildPrompt.
type CompressFunc func(basePrompt string, graphContext string, memoryContext string) string

// Planner uses Opus to create structured execution plans.
type Planner struct {
	client     *LLMClient
	compressor CompressFunc
	config     Config
}

func NewPlanner(client *LLMClient, compressor CompressFunc, config Config) *Planner {
	return &Planner{client: client, compressor: compressor, config: config}
}

// CreatePlan generates a structured plan for a task.
func (p *Planner) CreatePlan(task Task, decision *RoutingDecision) (*Plan, error) {
	systemPrompt := GetPlannerPrompt(task.TaskType, "", "", "")

	userMsg := task.Prompt
	if p.compressor != nil {
		userMsg = p.compressor(task.Prompt, "", "")
	}

	resp, err := p.client.Call(Opus, systemPrompt, userMsg, 1000)
	if err != nil {
		return nil, fmt.Errorf("planner opus call: %w", err)
	}

	plan, err := parsePlan(task.ID, resp.Content)
	if err != nil {
		// retry with stricter prompt
		strictSystem := systemPrompt + "\n\nCRITICAL: Output ONLY the JSON object. No explanation, no markdown, no code fences."
		resp2, err2 := p.client.Call(Opus, strictSystem, userMsg, 1000)
		if err2 != nil {
			return nil, fmt.Errorf("planner retry: %w", err2)
		}
		plan, err = parsePlan(task.ID, resp2.Content)
		if err != nil {
			// fallback: single-step plan wrapping the raw response
			return singleStepPlan(task), nil
		}
	}

	if err := ValidatePlan(plan); err != nil {
		breakCycles(plan)
	}

	return plan, nil
}

func parsePlan(taskID string, raw string) (*Plan, error) {
	text := stripCodeFences(raw)
	// find JSON object
	start := strings.Index(text, "{")
	if start == -1 {
		return nil, fmt.Errorf("no JSON object in response")
	}
	text = text[start:]

	var p Plan
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		return nil, fmt.Errorf("unmarshal plan: %w", err)
	}
	p.TaskID = taskID
	return &p, nil
}

func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		idx := strings.Index(s[3:], "\n")
		if idx != -1 {
			s = s[3+idx+1:]
		}
		if strings.HasSuffix(s, "```") {
			s = s[:len(s)-3]
		}
	}
	return strings.TrimSpace(s)
}

func singleStepPlan(task Task) *Plan {
	return &Plan{
		TaskID: task.ID,
		SubTasks: []SubTask{
			{
				ID:        "step_1",
				Action:    "modify_file",
				DependsOn: nil,
				Spec:      map[string]interface{}{"prompt": task.Prompt},
				VerifyTier: VerifySpotCheck,
			},
		},
	}
}

// ValidatePlan checks a plan for structural correctness.
func ValidatePlan(plan *Plan) error {
	if len(plan.SubTasks) == 0 {
		return fmt.Errorf("plan has no sub-tasks")
	}
	if len(plan.SubTasks) > 5 {
		return fmt.Errorf("plan has %d sub-tasks (max 5)", len(plan.SubTasks))
	}

	ids := make(map[string]bool)
	for _, st := range plan.SubTasks {
		if st.ID == "" {
			return fmt.Errorf("sub-task missing id")
		}
		if ids[st.ID] {
			return fmt.Errorf("duplicate sub-task id: %s", st.ID)
		}
		ids[st.ID] = true
		if st.VerifyTier < 1 || st.VerifyTier > 3 {
			return fmt.Errorf("sub-task %s has invalid verify_tier %d", st.ID, st.VerifyTier)
		}
		validActions := map[string]bool{
			"modify_file": true, "create_file": true, "run_test": true,
			"generate_test": true, "generate_pr": true, "analyze": true,
		}
		if !validActions[st.Action] {
			return fmt.Errorf("sub-task %s has unknown action: %s", st.ID, st.Action)
		}
	}

	// check depends_on references exist
	for _, st := range plan.SubTasks {
		for _, dep := range st.DependsOn {
			if !ids[dep] {
				return fmt.Errorf("sub-task %s depends_on unknown id: %s", st.ID, dep)
			}
		}
	}

	// check for cycles via DFS
	if hasCycle(plan.SubTasks) {
		return fmt.Errorf("plan has circular dependencies")
	}

	return nil
}

func hasCycle(tasks []SubTask) bool {
	edges := make(map[string][]string)
	for _, t := range tasks {
		edges[t.ID] = t.DependsOn
	}

	color := make(map[string]int) // 0=white, 1=gray, 2=black

	var dfs func(id string) bool
	dfs = func(id string) bool {
		color[id] = 1
		for _, dep := range edges[id] {
			if color[dep] == 1 {
				return true
			}
			if color[dep] == 0 && dfs(dep) {
				return true
			}
		}
		color[id] = 2
		return false
	}

	for _, t := range tasks {
		if color[t.ID] == 0 && dfs(t.ID) {
			return true
		}
	}
	return false
}

func breakCycles(plan *Plan) {
	// simple: remove all depends_on to break any cycle
	for i := range plan.SubTasks {
		plan.SubTasks[i].DependsOn = nil
	}
}
