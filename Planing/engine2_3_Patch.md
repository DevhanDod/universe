# Engine 2 & 3 — Alignment Patch

## Apply These Changes to engine2.md and engine3.md

**Purpose:** Fix the alignment issues identified during architecture review.  
**Engine 2:** Memory becomes personal (your sessions, not the team's)  
**Engine 3:** Skills require premium model verification before use  

---

# PART 1: ENGINE 2 — MEMORY CHANGES

## Change Summary

| # | What | Action | Why |
|---|------|--------|-----|
| 1 | Shared memory concept | REMOVE | Memory is personal — your past sessions, not Alice's fixes visible to Bob |
| 2 | `shared` column behavior | MODIFY | Column stays but always defaults to false. No cross-developer queries |
| 3 | Description and examples | MODIFY | Reframe as "better compacting" — saves your chat to DB so you don't lose context |
| 4 | Privacy filtering in queries | SIMPLIFY | No need for shared/private logic — everything is scoped to one developer |
| 5 | Team sharing MCP tool behavior | MODIFY | `store_observation` no longer has a `shared` parameter |

---

### Change 1: Update the Engine 2 description (Section 1)

**REMOVE this text from engine2.md section 1:**

```
Later, when anyone on the team works on that same function — or anything 
connected to it — the system automatically finds that note and injects 
it into the AI's prompt.
```

**REPLACE with:**

```
Later, when YOU come back to that same function — or anything connected 
to it — the system automatically finds your past notes and injects them 
into the AI's prompt. This is better than Cursor's compacting: instead 
of summarizing and losing detail, Universe saves the full context to a 
database and recalls it precisely when you need it.

This is YOUR memory — your past sessions, your fixes, your decisions. 
Each developer has their own private memory space. No one else sees 
your observations unless you explicitly choose to share them later.
```

---

### Change 2: Update the observations table

**In the SQL migration (Section 5 of engine2.md), modify the `shared` column:**

```sql
-- BEFORE (in engine2.md):
shared          BOOLEAN NOT NULL DEFAULT false,

-- AFTER (no schema change, just the comment):
-- Shared flag — reserved for future team sharing feature.
-- V1: always false. Every observation is private to the developer.
-- Future: developer can opt-in to share specific observations.
shared          BOOLEAN NOT NULL DEFAULT false,
```

**REMOVE this index** (not needed in V1 since shared is always false):

```sql
-- REMOVE this line:
CREATE INDEX idx_obs_shared ON observations(shared) WHERE shared = true;
```

---

### Change 3: Simplify store.go queries

**In engine2.md Section 6 (store.go), update ALL query functions to remove shared filtering.**

**BEFORE (every query had this WHERE clause):**

```sql
AND (shared = true OR developer_id = $2)
```

**AFTER (simpler — just filter by developer):**

```sql
AND developer_id = $1
```

**Specifically, update these functions:**

**GetByGraphNode:**

```go
// BEFORE:
// SQL:
//   SELECT id, graph_node_id, category, summary, confidence, created_at
//   FROM observations
//   WHERE graph_node_id = $1
//     AND (shared = true OR developer_id = $2)
//     AND confidence > 0.1
//   ORDER BY confidence DESC, created_at DESC
//   LIMIT $3

// AFTER:
// SQL:
//   SELECT id, graph_node_id, category, summary, confidence, created_at
//   FROM observations
//   WHERE graph_node_id = $1
//     AND developer_id = $2
//     AND confidence > 0.1
//   ORDER BY confidence DESC, created_at DESC
//   LIMIT $3
```

**GetByGraphNodes:**

```go
// BEFORE:
//   WHERE graph_node_id = ANY($1)
//     AND (shared = true OR developer_id = $2)

// AFTER:
//   WHERE graph_node_id = ANY($1)
//     AND developer_id = $2
```

**SearchKeyword:**

```go
// BEFORE:
//   WHERE fts @@ plainto_tsquery('english', $1)
//     AND (shared = true OR developer_id = $2)

// AFTER:
//   WHERE fts @@ plainto_tsquery('english', $1)
//     AND developer_id = $2
```

**SearchSemantic:**

```go
// BEFORE:
//   WHERE (shared = true OR developer_id = $2)

// AFTER:
//   WHERE developer_id = $1
//   (shift parameter numbers since we removed the shared check)
```

---

### Change 4: Update retriever.go Search method

**In engine2.md Section 8 (retriever.go), update the Search function description.**

**REMOVE this from the STEP 2 descriptions:**

```
Search A — Graph node match:
  graphResults := store.GetByGraphNodes(expandedNodes, query.DeveloperID, query.Limit * 2)
```

The `query.DeveloperID` is now REQUIRED, not optional. Add validation:

```go
// Add to the top of Search():
//
//   STEP 0: VALIDATE
//   If query.DeveloperID is empty:
//     Return error: "DeveloperID is required — memory is personal"
```

---

### Change 5: Update SearchQuery type

**In engine2.md Section 5 (types.go), update the SearchQuery struct comment:**

```go
// BEFORE:
// DeveloperID string
// // Filter by developer. Empty means all developers.
// // When shared=false observations exist, only the requesting developer's
// // private observations + all shared observations are returned.

// AFTER:
// DeveloperID string
// // REQUIRED. Scopes the search to this developer's observations only.
// // Each developer has their own private memory space.
// // Empty DeveloperID returns an error.
```

---

### Change 6: Update the MCP tool — store_observation

**In engine2.md Section 12, update the store_observation tool:**

**REMOVE the `shared` parameter:**

```json
{
  "name": "store_observation",
  "description": "Store an observation from your current session — a pattern, decision, convention, or fix. This saves to YOUR personal memory and will be recalled in your future sessions when you work on the same code.",
  "input_schema": {
    "type": "object",
    "properties": {
      "graph_node_id": {
        "type": "string",
        "description": "The graph node this observation relates to."
      },
      "category": {
        "type": "string",
        "enum": ["fix", "pattern", "decision", "failure", "convention"],
        "description": "What kind of observation this is."
      },
      "content": {
        "type": "string",
        "description": "The observation text. Will be stored as your personal memory."
      }
    },
    "required": ["graph_node_id", "category", "content"]
  }
}
```

**Note:** The `shared` field is removed from the MCP tool input. Observations are always private.

---

### Change 7: Update the recall_memory MCP tool description

```json
{
  "name": "recall_memory",
  "description": "Search YOUR past observations from previous sessions. Returns compact summaries first. Use get_observation_details to load full details for specific IDs. This is your personal memory — only your own past sessions are searched."
}
```

---

### Change 8: Update examples throughout engine2.md

**REMOVE all cross-developer examples. Replace with single-developer examples.**

**REMOVE examples like:**

```
"Alice fixed this 2 weeks ago — changed int to string"
"Bob's agent picks up Alice's observation automatically"
"Cross-developer memory sharing"
```

**REPLACE with:**

```
"You fixed this 2 weeks ago — changed int to string"
"Your past session is recalled when you touch the same function"
"Your personal session history, better than Cursor's compacting"
```

**Specifically update the MCP auto-injection description (Section 12):**

```
BEFORE:
  "When a new MCP session begins, automatically call retriever.GetSessionContext 
   to inject relevant past observations from any developer"

AFTER:
  "When a new MCP session begins, automatically call retriever.GetSessionContext 
   to inject YOUR relevant past observations. The developer_id comes from the 
   session/config — only your own memories are recalled."
```

---

### Change 9: Update the MemoryStats struct

**In engine2.md Section 6 (store.go), simplify MemoryStats:**

```go
// BEFORE:
type MemoryStats struct {
    TotalObservations   int
    ByCategory          map[string]int
    ByRepo              map[string]int
    AverageConfidence   float64
    OldestObservation   time.Time
    NewestObservation   time.Time
    TotalRecalls        int
    SharedObservations  int     // ← REMOVE THIS
}

// AFTER:
type MemoryStats struct {
    TotalObservations   int
    ByCategory          map[string]int
    ByRepo              map[string]int
    AverageConfidence   float64
    OldestObservation   time.Time
    NewestObservation   time.Time
    TotalRecalls        int
    // SharedObservations removed — V1 is personal memory only
}
```

---

### Change 10: Update tests

**In engine2.md Section 14 (testing), update Test 6:**

```go
// BEFORE:
// Test 6: Private/shared filtering
func TestStore_PrivateSharedFiltering(t *testing.T) {
    // Insert private observation for developer A
    // Insert shared observation for developer A
    // Search as developer B should find only the shared one
    // Search as developer A should find both
}

// AFTER:
// Test 6: Developer isolation
func TestStore_DeveloperIsolation(t *testing.T) {
    // Insert observation for developer A
    // Insert observation for developer B
    // Search as developer A should find ONLY developer A's observation
    // Search as developer B should find ONLY developer B's observation
    // No cross-developer leakage
}
```

---

### Engine 2 changes — complete list

| File in spec | Changes |
|-------------|---------|
| Section 1 (description) | Reframe as personal memory, not team sharing |
| Section 5 (migration SQL) | Update shared column comment, remove shared index |
| Section 5 (types.go) | Make DeveloperID required in SearchQuery, remove SharedObservations from stats |
| Section 6 (store.go) | Simplify ALL queries: remove `shared = true OR` clause, just filter by developer_id |
| Section 8 (retriever.go) | Add DeveloperID validation, remove shared logic |
| Section 12 (MCP tools) | Remove `shared` param from store_observation, update descriptions |
| Section 14 (tests) | Replace shared/private test with developer isolation test |
| All examples | Replace cross-developer examples with personal memory examples |

---

---

# PART 2: ENGINE 3 — SKILLS CHANGES

## Change Summary

| # | What | Action | Why |
|---|------|--------|-----|
| 1 | Skill description | MODIFY | Skills are reference knowledge, not auto-applied recipes |
| 2 | find_skill response | ADD | Include `requires_verification` flag and `verification_prompt` |
| 3 | Skill application flow | MODIFY | Premium model must verify before execution model uses |
| 4 | ModeSkillExecute concept | REMOVE | No more "skip planning, Haiku follows recipe" — skills always go through the planner |
| 5 | Confidence threshold for auto-use | REMOVE | No skill is ever used without premium model review |

---

### Change 1: Update the Engine 3 description (Section 1)

**REMOVE this text from engine3.md section 1:**

```
Next time anyone on the team hits a similar problem, the agent follows 
the recipe instead of thinking from scratch — saving tokens and time.
```

**REPLACE with:**

```
Next time you (or a teammate) hit a similar problem, the planning agent 
(premium model) can review the saved recipe and decide if it still 
applies. If the premium model approves the skill, the execution agent 
(low-cost model) follows it. Skills are NEVER used without premium 
model verification — they're reference knowledge, not auto-applied 
recipes.

Think of skills like a team wiki: useful references that a senior 
engineer (Opus) checks before handing to a junior engineer (Haiku) 
to follow.
```

---

### Change 2: Update the SkillSummary type

**In engine3.md Section 6 (types.go), add verification fields to SkillSummary:**

```go
// ADD these fields to SkillSummary:
type SkillSummary struct {
    ID           string        `json:"id"`
    Name         string        `json:"name"`
    Version      int           `json:"version"`
    Evolution    EvolutionType `json:"evolution"`
    TriggerDesc  string        `json:"trigger_desc"`
    Language     string        `json:"language"`
    Confidence   float64       `json:"confidence"`
    SuccessRate  float64       `json:"success_rate"`
    GraphOverlap float64       `json:"graph_overlap"`
    SearchScore  float64       `json:"search_score"`
    IsFrozen     bool          `json:"is_frozen"`

    // NEW: Verification fields
    RequiresVerification bool   `json:"requires_verification"`
    VerificationPrompt   string `json:"verification_prompt"`
}
```

---

### Change 3: Update the MatchResult type

**In engine3.md Section 6 (types.go), update MatchResult:**

```go
// BEFORE:
type MatchResult struct {
    BestMatch            *SkillSummary
    Candidates           []SkillSummary
    ExplorationTriggered bool
    SearchMethod         string
}

// AFTER:
type MatchResult struct {
    BestMatch            *SkillSummary   `json:"best_match"`
    Candidates           []SkillSummary  `json:"candidates"`
    ExplorationTriggered bool            `json:"exploration_triggered"`
    SearchMethod         string          `json:"search_method"`

    // NEW: Verification guidance for the planning agent
    VerificationRequired bool   `json:"verification_required"`
    VerificationMessage  string `json:"verification_message"`
}
```

---

### Change 4: Update matcher.go — Match function

**In engine3.md Section 8 (matcher.go), add verification fields to the return value.**

**ADD to the end of the Match function (after STEP 7: SORT AND RETURN):**

```go
//   STEP 8: ADD VERIFICATION METADATA
//     For EVERY matched skill, set:
//       RequiresVerification = true
//       VerificationPrompt = buildVerificationPrompt(skill, query)
//
//     The verification prompt tells the premium model what to check:
//       "Skill 'cross-repo-type-fix v3' was last updated [date].
//        It covers graph nodes: [list].
//        Success rate: [X%] over [N] applications.
//        [If has graph_changed tag: WARNING — the code this skill covers
//         has changed since the skill was last updated. Verify carefully.]
//        [If confidence < 0.7: NOTE — this skill is still building confidence.
//         Only [N] successful applications so far.]
//
//        Before applying this skill, verify:
//        1. Does the skill instruction match the CURRENT code structure?
//        2. Are the file paths and function names still correct?
//        3. Is the approach still the best way to solve this type of problem?"
//
//     Set on the MatchResult:
//       VerificationRequired = true
//       VerificationMessage = "Skill found but requires premium model verification 
//                              before the execution model can use it."
```

**ADD a new function:**

```go
// buildVerificationPrompt generates the prompt that tells the premium model
// what to check about a skill before approving it for execution.
//
// Parameters:
//   - skill: the matched skill
//   - query: the current task context
//
// Returns: a verification prompt string
//
// The prompt includes:
//   1. Skill metadata (name, version, last updated, success rate)
//   2. Graph node coverage (which functions it covers)
//   3. Warnings (stale code, low confidence, recent failures)
//   4. Three verification questions the premium model should answer:
//      a. Does the instruction match the current code?
//      b. Are file paths and function names still correct?
//      c. Is the approach still optimal?
//   5. Instructions: "If all checks pass, tell the execution agent to 
//      follow this skill. If any check fails, plan from scratch using 
//      the graph context instead."
func buildVerificationPrompt(skill SkillSummary, query MatchQuery) string
```

---

### Change 5: Update the find_skill MCP tool

**In engine3.md Section 14, update the find_skill tool response:**

```json
{
  "name": "find_skill",
  "description": "Search for a matching skill recipe. If found, the PLANNING agent (premium model) must verify the skill is still correct before the EXECUTION agent (low-cost model) uses it. Skills are reference knowledge, not auto-applied recipes."
}
```

**Update the FindSkillOutput in mcp-server.md (tools_skills.go):**

```go
// BEFORE:
type FindSkillOutput struct {
    Found              bool
    SkillID            string
    SkillName          string
    Version            int
    Instruction        string
    SuccessRate        float64
    Confidence         float64
    ExplorationSkipped bool
    Message            string
}

// AFTER:
type FindSkillOutput struct {
    Found              bool    `json:"found"`
    SkillID            string  `json:"skill_id,omitempty"`
    SkillName          string  `json:"skill_name,omitempty"`
    Version            int     `json:"version,omitempty"`
    Instruction        string  `json:"instruction,omitempty"`
    SuccessRate        float64 `json:"success_rate,omitempty"`
    Confidence         float64 `json:"confidence,omitempty"`
    ExplorationSkipped bool    `json:"exploration_skipped"`
    Message            string  `json:"message"`

    // NEW: Verification requirement
    RequiresVerification bool   `json:"requires_verification"`
    VerificationPrompt   string `json:"verification_prompt,omitempty"`
    StaleWarning         bool   `json:"stale_warning"`
    LastUpdated          string `json:"last_updated,omitempty"`
    TimesApplied         int    `json:"times_applied,omitempty"`
}
```

**Update the HandleFindSkill function logic:**

```go
// BEFORE (in mcp-server.md):
// When a skill is found:
//   return FindSkillOutput{
//       Found: true,
//       Instruction: skill.Instruction,
//       Message: "Skill found. Follow the instruction below for this task.",
//   }

// AFTER:
// When a skill is found:
//   return FindSkillOutput{
//       Found:                true,
//       SkillID:              skill.ID,
//       SkillName:            skill.Name,
//       Version:              skill.Version,
//       Instruction:          skill.Instruction,
//       SuccessRate:          result.BestMatch.SuccessRate,
//       Confidence:           result.BestMatch.Confidence,
//       RequiresVerification: true,  // ALWAYS true
//       VerificationPrompt:   result.BestMatch.VerificationPrompt,
//       StaleWarning:         hasGraphChangedTag(skill),
//       LastUpdated:          skill.CreatedAt.Format("2006-01-02"),
//       TimesApplied:         skill.TimesApplied,
//       Message: "Skill found. IMPORTANT: You are the planning agent " +
//                "(premium model). Review the skill instruction against " +
//                "the current code before including it in your plan. " +
//                "If the skill is correct, include its steps in the plan " +
//                "for the execution agent. If it's outdated, plan from " +
//                "scratch using the graph context instead.",
//   }
```

---

### Change 6: Update executor.go — Apply function

**In engine3.md Section 9 (executor.go), update the Apply function description.**

**BEFORE:**

```go
// Apply applies a skill to a task.
// The returned systemPrompt is passed to the LLM (Haiku in Engine 5).
```

**AFTER:**

```go
// Apply formats a skill for inclusion in a plan.
// 
// This does NOT directly send the skill to the execution agent.
// Instead, it formats the skill instruction for the PLANNING agent
// to review and optionally include in the plan.
//
// The flow is:
//   1. Planning agent calls find_skill → gets instruction + verification prompt
//   2. Planning agent reviews the skill against current code
//   3. If approved: planning agent includes the skill steps in the plan 
//      (stored via store_plan)
//   4. Execution agent gets the plan (via get_plan) which already contains 
//      the verified skill steps
//   5. Execution agent follows the plan — it doesn't know or care that 
//      the steps came from a skill
//
// The skill instruction is NEVER passed directly to the execution agent.
// It's always filtered through the planning agent's review.
```

---

### Change 7: Update the skill application examples

**REMOVE this example from engine3.md (wherever it appears):**

```
Agent finds skill → follows it immediately → saves tokens
```

**REPLACE with:**

```
Planner (premium) finds skill → verifies it's still correct → 
includes verified steps in the plan → Executor (cheap) follows 
the plan → planner verifies the result
```

---

### Change 8: Update the Cursor Rules reference

**The planner Cursor Rule must include skill verification behavior. Add this to the rule description in engine3.md Section 14:**

```
# In .cursor/rules/universe-planner.mdc, add:

When find_skill returns a match:
1. READ the skill instruction carefully
2. CHECK: does this instruction match the current code? 
   (use the graph context from get_dependencies)
3. CHECK: are the file paths and function names still correct?
4. If the skill has stale_warning=true, be EXTRA careful — 
   the code has changed since this skill was written
5. If the skill passes your review:
   - Include its steps in your plan (via store_plan)
   - Note in the plan: "Steps 2-4 based on skill: type-fix-v3"
6. If the skill fails your review:
   - Ignore it and plan from scratch using graph context
   - Call report_skill_execution with success=false and 
     error_detail="Skill outdated: [what's wrong]"
```

---

### Change 9: Update the confidence behavior description

**In engine3.md Section 6 (types.go), update the confidence comment:**

```go
// BEFORE:
// Tentative confidence: starts at 0.5 for new skills.
// Reaches 1.0 after 5+ successful applications.
// Formula: min(1.0, 0.5 + (times_succeeded * 0.1))

// AFTER:
// Tentative confidence: starts at 0.5 for new skills.
// Reaches 1.0 after 5+ successful applications.
// Formula: min(1.0, 0.5 + (times_succeeded * 0.1))
//
// IMPORTANT: Confidence does NOT determine whether a skill is 
// auto-applied. ALL skills require premium model verification 
// regardless of confidence. Confidence is used for:
//   1. RANKING — higher confidence skills appear first in search results
//   2. DASHBOARD — shows managers which skills are well-tested
//   3. PRUNING — very low confidence + zero usage = eligible for cleanup
//   4. VERIFICATION PROMPT — low confidence skills get extra warnings 
//      in the verification prompt ("only 2 successful uses so far")
```

---

### Change 10: Update the Config defaults description

**In engine3.md Section 6 (types.go), update these config fields:**

```go
// BEFORE:
MinConfidenceForMatch     float64 // Minimum confidence to use a skill. Default: 0.5
MinSuccessRateForMatch    float64 // Minimum success rate. Default: 0.60

// AFTER:
MinConfidenceForMatch     float64 // Minimum confidence to SHOW a skill in results. Default: 0.3
                                   // (lower threshold since premium model will verify anyway)
MinSuccessRateForMatch    float64 // Minimum success rate to SHOW a skill. Default: 0.40
                                   // (lower threshold — let premium model see more options 
                                   //  and decide. It's better to show a 50% skill with a 
                                   //  warning than to hide it.)
```

**Update DefaultConfig():**

```go
// BEFORE:
MinConfidenceForMatch:      0.5,
MinSuccessRateForMatch:     0.60,

// AFTER:
MinConfidenceForMatch:      0.3,   // Lower — premium model will verify
MinSuccessRateForMatch:     0.40,  // Lower — let premium model see more options
```

---

### Change 11: Update tests

**In engine3.md Section 17 (testing), add a new test:**

```go
// ADD this test:

// Test 31: find_skill always returns requires_verification = true
func TestFindSkill_AlwaysRequiresVerification(t *testing.T) {
    // Insert a skill with 100% success rate and 1.0 confidence
    // Call matcher.Match()
    // Verify: result.BestMatch.RequiresVerification == true
    // Even a perfect skill needs verification
}

// Test 32: find_skill includes verification prompt
func TestFindSkill_IncludesVerificationPrompt(t *testing.T) {
    // Insert a skill
    // Call matcher.Match()
    // Verify: result.BestMatch.VerificationPrompt is not empty
    // Verify: prompt contains "verify", "current code", file names
}

// Test 33: Stale skill gets extra warning in verification prompt
func TestFindSkill_StaleSkillWarning(t *testing.T) {
    // Insert skill, mark as stale (graph_changed negative tag)
    // Call matcher.Match()
    // Verify: VerificationPrompt contains "WARNING" and "code has changed"
    // Verify: StaleWarning == true in the output
}
```

---

### Engine 3 changes — complete list

| File in spec | Changes |
|-------------|---------|
| Section 1 (description) | Reframe skills as reference knowledge needing premium approval |
| Section 6 (types.go) | Add RequiresVerification + VerificationPrompt to SkillSummary and MatchResult. Update confidence comment. Lower threshold defaults. |
| Section 8 (matcher.go) | Add STEP 8 (verification metadata). Add buildVerificationPrompt function. |
| Section 9 (executor.go) | Update Apply() description — skill goes through planner, not directly to executor |
| Section 14 (MCP tools) | Update find_skill description and response format with verification fields |
| Section 17 (tests) | Add 3 new tests for verification behavior |
| Cursor Rules reference | Add skill verification steps to planner rule |
| All examples | Replace "agent follows skill immediately" with "planner verifies, then includes in plan" |

---

---

# PART 3: CROSS-ENGINE ALIGNMENT NOTES

These changes affect how Engine 2 and 3 interact with other specs. Note these when updating the other spec files later:

### For mcp-server.md (update later):
- `recall_memory` tool: update description to say "YOUR past observations"
- `store_observation` tool: remove `shared` parameter
- `find_skill` tool: update response type to include verification fields
- All tool descriptions should reflect personal memory and skill verification

### For dashboard.md (update later):
- Memory view: show "Your Observations" not "Team Observations"
- Memory filters: remove "by developer" dropdown (you only see your own)
- Skills view: add a "Verification required" badge on every skill
- Plans view: show which skills were used in plans (post-verification)

### For engine5.md (rewrite later):
- Remove ModeSkillExecute routing mode entirely
- Skills flow through the plan: find_skill → planner verifies → store_plan includes skill steps → executor follows plan
- The plan table should track `skill_used` (which skill the planner approved)

### For cli-wiring.md (update later):
- `universe status` Engine 2 line: show "X personal observations" not "X shared"
- `universe status` Engine 3 line: show "X skills (all require verification)"

### For local-test.md (update later):
- Memory test: verify developer isolation (dev A can't see dev B's data)
- Skills test: verify find_skill always returns requires_verification=true
