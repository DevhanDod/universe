package skills

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strings"
	"time"
)

// LLMClient is an interface to call the premium LLM (Opus).
// Implemented by Engine 5's orchestrator to break circular dependency.
type LLMClient interface {
	CallOpus(systemPrompt, userMessage string, maxTokens int) (string, error)
}

// Evolver handles skill evolution — CAPTURED and FIX modes.
type Evolver struct {
	store    *Store
	embedder EmbedFunc
	llm      LLMClient
	safety   *SafetyScanner
	config   Config
}

// NewEvolver creates a new Evolver.
func NewEvolver(store *Store, embedder EmbedFunc, llm LLMClient, safety *SafetyScanner, config Config) *Evolver {
	return &Evolver{store: store, embedder: embedder, llm: llm, safety: safety, config: config}
}

// OnExecutionComplete is called after every skill execution and decides whether to evolve.
func (e *Evolver) OnExecutionComplete(req EvolutionRequest) (*EvolutionResult, error) {
	// No skill applied → try CAPTURE if successful
	if req.AppliedSkill == nil {
		if req.Execution.Success {
			return e.tryCaptureSkill(req)
		}
		return &EvolutionResult{Action: "skipped", Reason: "no skill applied and task failed"}, nil
	}

	// Skill succeeded → no evolution needed
	if req.Execution.Success {
		return &EvolutionResult{Action: "skipped", Reason: "skill succeeded"}, nil
	}

	skill := req.AppliedSkill

	// Frozen → don't evolve
	if skill.IsFrozen {
		return &EvolutionResult{Action: "skipped", Reason: "skill is frozen"}, nil
	}

	// Daily limit check
	if skill.EvolutionAttemptsToday >= e.config.MaxEvolutionAttemptsPerDay {
		if err := e.store.FreezeSkill(skill.ID); err != nil {
			log.Printf("evolver: freeze skill %s: %v", skill.ID, err)
		}
		return &EvolutionResult{
			Action: "frozen",
			Reason: fmt.Sprintf("hit daily evolution limit (%d)", e.config.MaxEvolutionAttemptsPerDay),
		}, nil
	}

	// Check consecutive failures
	consecutive, err := e.store.GetConsecutiveFailures(skill.ID)
	if err != nil {
		return nil, err
	}
	if consecutive >= e.config.ConsecutiveFailuresForFix {
		return e.tryFixSkill(skill, req)
	}

	// Check failure rate over last 10
	rate, err := e.store.GetRecentFailureRate(skill.ID, 10)
	if err != nil {
		return nil, err
	}
	if rate >= e.config.FailureRateForFix {
		return e.tryFixSkill(skill, req)
	}

	return &EvolutionResult{Action: "skipped", Reason: "not enough failures yet"}, nil
}

// ── CAPTURED ──────────────────────────────────────────────────────────────────

func (e *Evolver) tryCaptureSkill(req EvolutionRequest) (*EvolutionResult, error) {
	// STEP 1: Minimum events check
	if len(req.SessionEvents) < 3 {
		return &EvolutionResult{Action: "skipped", Reason: "session too short (< 3 events)"}, nil
	}
	if len(req.Execution.GraphNodeIDs) == 0 {
		return &EvolutionResult{Action: "skipped", Reason: "no graph node IDs in execution"}, nil
	}

	// STEP 2: Duplicate check via semantic search
	if e.embedder != nil && req.Execution.TaskPrompt != "" {
		embedding, err := e.embedder(req.Execution.TaskPrompt)
		if err == nil {
			existing, err := e.store.SearchSemantic(embedding, 0, 5)
			if err == nil && len(existing) > 0 && existing[0].SearchScore > e.config.SimilarityThresholdForSkip {
				return &EvolutionResult{
					Action: "skipped",
					Reason: fmt.Sprintf("similar skill already exists: %s (score %.2f)", existing[0].Name, existing[0].SearchScore),
				}, nil
			}
		}
	}

	// STEP 3: Graph node capacity check
	for _, nodeID := range req.Execution.GraphNodeIDs {
		count, err := e.store.CountSkillsForGraphNode(nodeID)
		if err != nil {
			continue
		}
		if count >= e.config.MaxSkillsPerGraphNode {
			return &EvolutionResult{
				Action: "skipped",
				Reason: fmt.Sprintf("max skills per node reached for %s (%d)", nodeID, e.config.MaxSkillsPerGraphNode),
			}, nil
		}
	}

	// STEP 4: Total skill limit
	total, err := e.store.GetTotalActiveSkills()
	if err == nil && total >= e.config.MaxTotalSkills {
		log.Printf("evolver: global skill limit reached (%d)", e.config.MaxTotalSkills)
		return &EvolutionResult{Action: "skipped", Reason: "global skill limit reached"}, nil
	}

	// STEP 5: Ask LLM to extract the skill
	if e.llm == nil {
		return &EvolutionResult{Action: "skipped", Reason: "no LLM client configured"}, nil
	}

	eventsJSON, _ := json.MarshalIndent(req.SessionEvents, "", "  ")
	userMsg := fmt.Sprintf("SESSION EVENTS:\n%s\n\nGRAPH CONTEXT:\n%s\n\nORIGINAL TASK: %s\nAGENT OUTPUT: %s",
		eventsJSON, req.GraphContext,
		truncate(req.Execution.TaskPrompt, 500),
		truncate(req.Execution.TaskOutput, 500),
	)

	systemPrompt := `Extract a reusable skill from this successful agent session.

Respond with ONLY this JSON:
{
  "name": "short-kebab-case-name",
  "trigger_desc": "one sentence: when to use this skill",
  "instruction": "step-by-step recipe the agent should follow",
  "language": "go or python or null if language-agnostic"
}`

	rawResponse, err := e.llm.CallOpus(systemPrompt, userMsg, 600)
	if err != nil {
		return nil, fmt.Errorf("capture: call opus: %w", err)
	}

	// STEP 6: Safety scan
	scanResult, err := e.safety.ScanEvolutionOutput(rawResponse)
	if err != nil || !scanResult.Safe {
		reason := "safety scan failed"
		if len(scanResult.Blocked) > 0 {
			reason = scanResult.Blocked[0]
		}
		return &EvolutionResult{Action: "skipped", Reason: reason}, nil
	}

	// Parse the validated JSON
	var parsed struct {
		Name        string `json:"name"`
		TriggerDesc string `json:"trigger_desc"`
		Instruction string `json:"instruction"`
		Language    string `json:"language"`
	}
	clean := cleanJSON(rawResponse)
	if err := json.Unmarshal([]byte(clean), &parsed); err != nil {
		return &EvolutionResult{Action: "skipped", Reason: "could not parse opus response"}, nil
	}

	// STEP 7: Embed and store
	skill := Skill{
		Name:         parsed.Name,
		Version:      1,
		Evolution:    EvolutionCaptured,
		GraphNodeIDs: req.Execution.GraphNodeIDs,
		Language:     parsed.Language,
		TriggerDesc:  parsed.TriggerDesc,
		Instruction:  parsed.Instruction,
		TestCase: &SkillTestCase{
			Input:          req.Execution.TaskPrompt,
			ExpectedOutput: req.Execution.TaskOutput,
			GraphNodeIDs:   req.Execution.GraphNodeIDs,
		},
		CreatedBy:  req.Execution.DeveloperID,
		Shared:     true,
		IsActive:   true,
		Confidence: e.config.ConfidenceStartValue,
	}

	if e.embedder != nil {
		if emb, err := e.embedder(parsed.TriggerDesc + " " + parsed.Instruction); err == nil {
			skill.Embedding = emb
		}
	}

	stored, err := e.store.InsertSkill(skill)
	if err != nil {
		return nil, fmt.Errorf("capture: insert skill: %w", err)
	}

	log.Printf("captured new skill %q v1 covering %d graph nodes from %s",
		stored.Name, len(stored.GraphNodeIDs), req.Execution.DeveloperID)

	return &EvolutionResult{
		Action:     "captured",
		NewSkillID: stored.ID,
		Reason:     fmt.Sprintf("extracted from %s session", req.Execution.DeveloperID),
	}, nil
}

// ── FIX ───────────────────────────────────────────────────────────────────────

func (e *Evolver) tryFixSkill(skill *Skill, req EvolutionRequest) (*EvolutionResult, error) {
	// STEP 1: Anti-loop check
	if skill.EvolutionAttemptsToday >= e.config.MaxEvolutionAttemptsPerDay {
		if err := e.store.FreezeSkill(skill.ID); err != nil {
			log.Printf("evolver: freeze %s: %v", skill.ID, err)
		}
		return &EvolutionResult{Action: "frozen", Reason: "daily evolution limit hit"}, nil
	}
	_ = e.store.IncrementEvolutionAttempts(skill.ID)

	// STEP 2: Gather recent failures
	failures, err := e.store.GetRecentExecutions(skill.ID, 3)
	if err != nil {
		return nil, err
	}
	var failureMsgs strings.Builder
	for i, f := range failures {
		if !f.Success {
			failureMsgs.WriteString(fmt.Sprintf("FAILURE %d: %s\n", i+1, truncate(f.ErrorDetail, 300)))
		}
	}

	if e.llm == nil {
		return &EvolutionResult{Action: "skipped", Reason: "no LLM client configured"}, nil
	}

	// STEP 3: Ask Opus to fix
	systemPrompt := `This skill has failed repeatedly. Fix the instruction.

Respond with ONLY this JSON:
{
  "trigger_desc": "updated trigger description",
  "instruction": "fixed step-by-step instruction",
  "what_changed": "one sentence: what you fixed and why"
}`

	userMsg := fmt.Sprintf("SKILL NAME: %s\nCURRENT INSTRUCTION:\n%s\n\n%s\nGRAPH CONTEXT:\n%s",
		skill.Name, skill.Instruction, failureMsgs.String(), req.GraphContext)

	rawResponse, err := e.llm.CallOpus(systemPrompt, userMsg, 800)
	if err != nil {
		return nil, fmt.Errorf("fix: call opus: %w", err)
	}

	// STEP 4: Validate against test case (if present)
	if skill.TestCase != nil && e.llm != nil {
		// Quick validation: call Haiku with new instruction + test input
		// We check structurally — if Opus output looks plausible, proceed
		// Full test-case validation deferred to Engine 5 integration
		_ = skill.TestCase // placeholder — full impl in MCP integration
	}

	// STEP 5: Safety scan
	scanResult, err := e.safety.ScanEvolutionOutput(rawResponse)
	if err != nil || !scanResult.Safe {
		reason := "safety scan failed"
		if len(scanResult.Blocked) > 0 {
			reason = scanResult.Blocked[0]
		}
		return &EvolutionResult{Action: "skipped", Reason: reason}, nil
	}

	var parsed struct {
		TriggerDesc string `json:"trigger_desc"`
		Instruction string `json:"instruction"`
		WhatChanged string `json:"what_changed"`
	}
	if err := json.Unmarshal([]byte(cleanJSON(rawResponse)), &parsed); err != nil {
		return &EvolutionResult{Action: "skipped", Reason: "could not parse opus fix response"}, nil
	}

	// STEP 6: Store new version, deactivate old
	newSkill := Skill{
		Name:         skill.Name,
		Version:      skill.Version + 1,
		ParentID:     &skill.ID,
		Evolution:    EvolutionFix,
		GraphNodeIDs: skill.GraphNodeIDs,
		Language:     skill.Language,
		TriggerDesc:  parsed.TriggerDesc,
		Instruction:  parsed.Instruction,
		TestCase:     skill.TestCase,
		CreatedBy:    "evolver",
		Shared:       skill.Shared,
		IsActive:     true,
		Confidence:   e.config.ConfidenceStartValue,
	}

	if e.embedder != nil {
		if emb, err := e.embedder(parsed.TriggerDesc + " " + parsed.Instruction); err == nil {
			newSkill.Embedding = emb
		}
	}

	if err := e.store.DeactivateSkill(skill.ID); err != nil {
		return nil, fmt.Errorf("fix: deactivate old: %w", err)
	}

	stored, err := e.store.InsertSkill(newSkill)
	if err != nil {
		// Re-activate old on failure
		_ = e.store.UnfreezeSkill(skill.ID)
		return nil, fmt.Errorf("fix: insert new version: %w", err)
	}

	log.Printf("fixed skill %q v%d → v%d: %s", skill.Name, skill.Version, stored.Version, parsed.WhatChanged)

	return &EvolutionResult{
		Action:     "fixed",
		NewSkillID: stored.ID,
		Reason:     parsed.WhatChanged,
	}, nil
}

// ── DERIVED (stub — per spec, implement after CAPTURED+FIX are stable) ────────

func (e *Evolver) tryDeriveSkill(parentSkill *Skill, req EvolutionRequest) (*EvolutionResult, error) {
	return &EvolutionResult{Action: "skipped", Reason: "DERIVED mode not yet implemented"}, nil
}

// ── default HTTP-based LLMClient ──────────────────────────────────────────────

// DefaultLLMClient is a simple HTTP client for Opus.
// Engine 5 will provide a richer implementation via the LLMClient interface.
type DefaultLLMClient struct {
	APIKey   string
	Model    string
	Endpoint string
	client   *http.Client
}

func NewDefaultLLMClient(apiKey, model, endpoint string) *DefaultLLMClient {
	if endpoint == "" {
		endpoint = "https://api.anthropic.com/v1/messages"
	}
	if model == "" {
		model = "claude-opus-4-20250514"
	}
	return &DefaultLLMClient{
		APIKey:   apiKey,
		Model:    model,
		Endpoint: endpoint,
		client:   &http.Client{Timeout: 60 * time.Second},
	}
}

func (c *DefaultLLMClient) CallOpus(systemPrompt, userMessage string, maxTokens int) (string, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"model":      c.Model,
		"max_tokens": maxTokens,
		"system":     systemPrompt,
		"messages":   []map[string]string{{"role": "user", "content": userMessage}},
	})
	req, err := http.NewRequest("POST", c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("opus request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("opus HTTP %d", resp.StatusCode)
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode opus: %w", err)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty opus response")
	}
	return result.Content[0].Text, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}
	return s
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
