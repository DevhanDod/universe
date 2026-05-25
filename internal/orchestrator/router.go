package orchestrator

import (
	"fmt"
	"strings"
)

// SkillMatcher finds the best skill for a task (implemented by Engine 3).
type SkillMatcher interface {
	Match(graphNodeIDs []string, taskText string) (*SkillMatch, error)
}

type SkillMatch struct {
	SkillID      string
	Name         string
	Instruction  string
	SuccessRate  float64
	GraphOverlap float64
}

// MemoryRecaller retrieves relevant past observations (implemented by Engine 2).
type MemoryRecaller interface {
	QuickCheck(graphNodeIDs []string, developerID string) (*MemoryCheck, error)
}

type MemoryCheck struct {
	HasExactMatch bool
	Confidence    float64
	Summary       string
}

// GraphAnalyzer provides structural complexity signals (adapter for Engine 1).
type GraphAnalyzer interface {
	CountAffectedNodes(nodeIDs []string) (int, error)
	IsCrossRepo(nodeIDs []string) (bool, error)
	GetLanguage(nodeIDs []string) (string, error)
}

// Router decides how to handle each task without LLM calls.
type Router struct {
	skills SkillMatcher
	memory MemoryRecaller
	graph  GraphAnalyzer
	config Config
}

func NewRouter(skills SkillMatcher, memory MemoryRecaller, graph GraphAnalyzer, config Config) *Router {
	return &Router{skills: skills, memory: memory, graph: graph, config: config}
}

// Route makes the routing decision for a task. Zero LLM tokens.
func (r *Router) Route(task Task) (*RoutingDecision, error) {
	// 1. SKILL MATCH
	if r.skills != nil && len(task.GraphNodeIDs) > 0 {
		match, err := r.skills.Match(task.GraphNodeIDs, task.Prompt)
		if err == nil && match != nil &&
			match.SuccessRate >= r.config.SkillMatchMinSuccessRate &&
			match.GraphOverlap >= 0.5 {
			return &RoutingDecision{
				Mode:           ModeSkillExecute,
				PlannerRole:    "low_cost",
				ExecutorRole:   "low_cost",
				VerifierRole:   "automated",
				NextStep:       "skill_execute",
				VerifyTier:     VerifyAutomated,
				SkipPlanning:   true,
				MatchedSkillID: match.SkillID,
				Reason:         "skill match: " + match.Name,
			}, nil
		}
	}

	// 2. MEMORY MATCH
	if r.memory != nil {
		check, err := r.memory.QuickCheck(task.GraphNodeIDs, task.DeveloperID)
		if err == nil && check != nil &&
			check.HasExactMatch && check.Confidence >= r.config.MemoryMatchMinConfidence {
			return &RoutingDecision{
				Mode:         ModeMemoryApply,
				PlannerRole:  "low_cost",
				ExecutorRole: "low_cost",
				VerifierRole: "low_cost",
				NextStep:     "memory_apply",
				VerifyTier:   VerifySpotCheck,
				SkipPlanning: true,
				MemoryHit:    true,
				Reason:       "memory hit: confidence " + fmtFloat(check.Confidence),
			}, nil
		}
	}

	// 3. COMPLEXITY CHECK
	nodeCount := len(task.GraphNodeIDs)
	crossRepo := false
	if r.graph != nil && len(task.GraphNodeIDs) > 0 {
		if n, err := r.graph.CountAffectedNodes(task.GraphNodeIDs); err == nil {
			nodeCount = n
		}
		if cr, err := r.graph.IsCrossRepo(task.GraphNodeIDs); err == nil {
			crossRepo = cr
		}
	}

	hasTemplate := HasTemplate(task.TaskType)
	simple := nodeCount <= r.config.SimpleTaskMaxNodes && !crossRepo

	switch {
	case simple && hasTemplate:
		return &RoutingDecision{
			Mode:         ModePlanExecute,
			PlannerRole:  "premium",
			ExecutorRole: "low_cost",
			VerifierRole: "automated",
			NextStep:     "plan",
			VerifyTier:   VerifyAutomated,
			TemplateID:   GetTemplateName(task.TaskType),
			Reason:       "simple task with template",
		}, nil

	case simple && !hasTemplate && nodeCount <= 2:
		return &RoutingDecision{
			Mode:         ModeSingleHaiku,
			PlannerRole:  "low_cost",
			ExecutorRole: "low_cost",
			VerifierRole: "low_cost",
			NextStep:     "direct",
			VerifyTier:   VerifySpotCheck,
			SkipPlanning: true,
			Reason:       "simple task, no template",
		}, nil

	case !simple && hasTemplate:
		return &RoutingDecision{
			Mode:         ModeFullOrchestration,
			PlannerRole:  "premium",
			ExecutorRole: "low_cost",
			VerifierRole: "premium",
			NextStep:     "plan",
			VerifyTier:   VerifyFullReview,
			TemplateID:   GetTemplateName(task.TaskType),
			Reason:       "complex task with template",
		}, nil

	default:
		return &RoutingDecision{
			Mode:         ModeSingleOpus,
			PlannerRole:  "premium",
			ExecutorRole: "premium",
			VerifierRole: "premium",
			NextStep:     "direct",
			VerifyTier:   VerifyFullReview,
			SkipPlanning: true,
			Reason:       "complex task, no template",
		}, nil
	}
}

// ClassifyTaskType determines the TaskType from the developer's prompt using keyword matching.
func ClassifyTaskType(prompt string) TaskType {
	lower := strings.ToLower(prompt)

	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(lower, w) {
				return true
			}
		}
		return false
	}

	switch {
	// explanation must come before analysis ("why"/"how does" appear in analysis prompts too)
	case has("explain", "why", "how does", "what is", "understand"):
		return TaskExplanation
	case has("fix", "bug", "mismatch", "broken", "panic", "crash"):
		return TaskCodeFix
	case has("test", "cover", "assert", "spec"):
		return TaskTestGen
	case has("pr", "pull request", "review"):
		return TaskPRGen
	case has("refactor", "rename", "extract", "restructure"):
		return TaskRefactor
	case has("migrate", "migration", "schema"):
		return TaskMigration
	// config before update/upgrade so "update the config" → config_change, not dep_update
	case has("config", "setting", "env var", ".env", "yaml", "toml"):
		return TaskConfigChange
	case has("update", "upgrade", "dependency", "dependencies", "version", "bump"):
		return TaskDepUpdate
	case has("analyze", "analysis", "impact", "affect"):
		return TaskAnalysis
	default:
		return TaskGeneral
	}
}

func fmtFloat(f float64) string {
	return fmt.Sprintf("%.2f", f)
}
