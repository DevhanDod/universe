package orchestrator

import (
	"fmt"
	"strings"
)

// Router provides RECOMMENDATIONS to the planner agent.
// It does NOT make routing decisions — the planner (premium model in
// Cursor) reads the recommendations and decides what to do.
//
// Zero LLM calls. All database queries.

// SkillChecker checks whether a matching skill exists.
type SkillChecker interface {
	HasMatch(graphNodeIDs []string, taskText string) (bool, string, string, float64, error)
	// Returns: found, skillID, skillName, confidence, error
}

// MemoryChecker checks whether relevant memory exists for a developer.
type MemoryChecker interface {
	HasRelevant(graphNodeIDs []string, developerID string) (bool, int, error)
	// Returns: found, count, error
}

// GraphChecker provides structural complexity signals.
type GraphChecker interface {
	CountAffectedNodes(nodeIDs []string) (int, error)
	IsCrossRepo(nodeIDs []string) (bool, error)
}

// Router holds references to the other engines for recommendation queries.
type Router struct {
	skills SkillChecker
	memory MemoryChecker
	graph  GraphChecker
}

// NewRouter creates a new recommendation router.
// All parameters are optional (nil-safe).
func NewRouter(skills SkillChecker, memory MemoryChecker, graph GraphChecker) *Router {
	return &Router{skills: skills, memory: memory, graph: graph}
}

// Recommend analyzes a task and returns recommendations for the planner.
// Zero LLM calls. All database queries.
func (r *Router) Recommend(graphNodeIDs []string, taskText string, developerID string) (*RoutingRecommendation, error) {
	rec := &RoutingRecommendation{}

	// Check skills
	if r.skills != nil {
		found, skillID, skillName, confidence, err := r.skills.HasMatch(graphNodeIDs, taskText)
		if err == nil && found {
			rec.SkillAvailable = true
			rec.SkillID = skillID
			rec.SkillName = skillName
			rec.SkillConfidence = confidence
		}
	}

	// Check memory
	if r.memory != nil {
		found, count, err := r.memory.HasRelevant(graphNodeIDs, developerID)
		if err == nil && found {
			rec.MemoryAvailable = true
			rec.MemoryCount = count
		}
	}

	// Graph analysis
	nodeCount := len(graphNodeIDs)
	crossRepo := false
	if r.graph != nil && len(graphNodeIDs) > 0 {
		if n, err := r.graph.CountAffectedNodes(graphNodeIDs); err == nil {
			nodeCount = n
		}
		if cr, err := r.graph.IsCrossRepo(graphNodeIDs); err == nil {
			crossRepo = cr
		}
	}
	rec.AffectedNodeCount = nodeCount
	rec.CrossRepo = crossRepo

	// Risk level
	switch {
	case nodeCount >= 6 && crossRepo:
		rec.RiskLevel = "high"
	case nodeCount >= 3 || crossRepo:
		rec.RiskLevel = "medium"
	default:
		rec.RiskLevel = "low"
	}

	// Build recommendation text
	var parts []string
	if rec.SkillAvailable {
		parts = append(parts, fmt.Sprintf("Skill '%s' available (%.0f%% confidence). Verify before using.",
			rec.SkillName, rec.SkillConfidence*100))
	}
	if rec.MemoryAvailable {
		parts = append(parts, fmt.Sprintf("%d past observations found. Check recall_memory.", rec.MemoryCount))
	}
	switch rec.RiskLevel {
	case "high":
		parts = append(parts, fmt.Sprintf("Cross-repo change, %d nodes affected. Plan carefully, high risk.", nodeCount))
	case "medium":
		if crossRepo {
			parts = append(parts, fmt.Sprintf("Cross-repo change affecting %d nodes. Medium risk.", nodeCount))
		} else {
			parts = append(parts, fmt.Sprintf("%d nodes affected. Medium risk.", nodeCount))
		}
	default:
		parts = append(parts, "Simple change. Low risk.")
	}
	rec.Recommendation = strings.Join(parts, " ")

	return rec, nil
}

// ClassifyTaskType determines what type of task this is based on keywords.
// Used for dashboard categorization, not routing decisions.
func ClassifyTaskType(prompt string) string {
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
	case has("fix", "bug", "broken", "crash", "panic"):
		return "code_fix"
	case has("test", "cover", "assert", "spec"):
		return "test_gen"
	case has("refactor", "rename", "extract", "restructure"):
		return "refactor"
	case has("migrate", "migration", "schema"):
		return "migration"
	case has("pr", "pull request"):
		return "pr_gen"
	case has("explain", "why", "how does", "what is"):
		return "explanation"
	default:
		return "general"
	}
}
