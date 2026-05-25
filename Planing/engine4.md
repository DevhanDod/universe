# Engine 4 — Compressed Communication Layer

## Build Specification for Claude Code

**Engine name:** Compressed Communication Layer  
**Concept:** Prompt Compression + Context Distillation (Microsoft LLMLingua, Anthropic Context Distillation)  
**Estimated effort:** Half a day  
**Dependencies:** Engine 1 (Knowledge Graph) — already built  
**Database required:** None  
**New services required:** None  

---

## 1. Context — What This Engine Does

Every token the AI agent generates costs money and time. Output tokens cost 3-5× more than input tokens on most models. This engine reduces token waste through three strategies:

1. **Compression prompts** — system-level instructions that strip filler, pleasantries, and hedging from agent output while keeping code and technical terms exact
2. **Graph shorthand** — replace verbose natural language descriptions of code relationships with compact graph-derived references (e.g., "the validate function in the auth-service repository which is called by 3 other services" becomes `auth.ValidateToken → 3 callers`)
3. **Structured output** — when the agent's output feeds another system (test runner, PR creator, sandbox), force JSON output instead of prose. Zero wasted tokens.

**Expected token savings:** ~75% reduction in output tokens, ~35% reduction in input tokens from graph shorthand injection.

---

## 2. Current Project Structure

This is the existing Universe project. Engine 4 adds a new `compress` package under `internal/`.

```
UNIVERSE/
├── .universe/
├── cmd/
│   └── (CLI entry point)
├── internal/
│   ├── analyzer/
│   │   └── analyzer.go              # Orchestrator — ties scanner + parser + extractor + graph
│   ├── extractor/
│   │   ├── extractor.go             # Extractor interface
│   │   ├── go_extractor.go          # Go-specific extraction
│   │   ├── python_extractor.go      # Python-specific extraction
│   ├── graph/
│   │   └── graph.go                 # Graph data structure (nodes + edges)
│   ├── models/
│   │   └── models.go                # Shared types: Node, Edge, ParseResult
│   ├── parser/
│   │   ├── go_parser.go
│   │   ├── parser.go                # Parser interface
│   │   ├── python_parser.go
│   │   ├── python_parser_fallback.go
│   │   └── registry.go              # Maps extensions → parsers
│   └── scanner/
│       └── scanner.go               # Directory walker
├── go.mod
├── go.sum
├── universe.exe
├── visualizer.html
├── UNIVERSE_PROJECT_SPEC.md
├── UNIVERSE_VISUALIZER_SPEC.md
└── UNIVERSE_FILE_CONTENT_UPDATE 02.md
```

---

## 3. New Files to Create

Add a new package `internal/compress/` with three files:

```
internal/
└── compress/
    ├── prompt.go          # Compression system prompt builder
    ├── shorthand.go       # Graph node → compact reference converter
    └── formatter.go       # Structured JSON output for machine consumers
```

No other existing files need modification to build Engine 4. Integration happens later when you build the MCP server and agent pipeline — this engine is a standalone library that those systems will import.

---

## 4. File: `internal/compress/prompt.go`

### Purpose
Builds the compression system prompt that gets injected into every LLM call. Supports three compression levels that control how aggressively the agent compresses its output.

### Types

```go
package compress

// CompressionLevel controls how aggressively the agent compresses output.
type CompressionLevel string

const (
    // LevelFull — machine-to-machine. JSON only, zero prose.
    // Use when output feeds another system (test runner, PR creator, sandbox).
    LevelFull CompressionLevel = "full"

    // LevelCompact — developer-facing but terse. Caveman mode.
    // Strips articles, pleasantries, hedging. Keeps code exact.
    // Default for most agent interactions.
    LevelCompact CompressionLevel = "compact"

    // LevelNormal — light compression. Friendly but efficient.
    // Removes obvious filler ("I'd be happy to help", "Sure, let me").
    // Keeps natural sentence structure. Good for explanations.
    LevelNormal CompressionLevel = "normal"
)
```

### Compression Prompt Templates

Each level has a different system prompt that gets prepended to the agent's instructions.

```go
// compactPrompt is the default compression prompt (Caveman-inspired).
// This is the primary token saver for developer-facing output.
const compactPrompt = `COMMUNICATION RULES — FOLLOW STRICTLY:
- Drop articles (a, an, the) from explanations
- No pleasantries: never say "I'd be happy to help", "Sure, let me", "Great question"
- No hedging: never say "It might be worth considering", "You may want to", "Perhaps"
- No filler: never say "Let me explain", "As you can see", "It's important to note"
- No apologies: never say "Sorry", "Unfortunately", "I apologize"
- Keep ALL code blocks, function names, variable names, error messages EXACT — never compress code
- Keep ALL technical terms exact — never simplify jargon
- Use graph shorthand when referencing code entities (provided below)
- Max 2 sentences for explanations unless specifically asked for detail
- If code alone answers the question, output code only — no surrounding prose
- Git commit messages, PR titles, PR descriptions: write normally (not compressed)
`

// normalPrompt is a lighter compression for explanation-heavy responses.
const normalPrompt = `COMMUNICATION RULES:
- Do not start responses with "I'd be happy to help", "Sure!", "Great question!", or similar
- Do not hedge with "It might be worth considering" or "You may want to"
- Be direct. State what is, not what might be.
- Keep code blocks and technical terms exact
- Use graph shorthand when referencing code entities (provided below)
`

// fullPrompt forces pure structured output — zero prose tokens.
const fullPrompt = `OUTPUT FORMAT — STRICT:
- Respond ONLY with valid JSON matching the provided schema
- No explanation before or after the JSON
- No markdown code fences around the JSON
- No preamble, no summary, no sign-off
- Every token must be part of the JSON structure
- If you cannot complete the task, respond with: {"error": "description"}
`
```

### Functions

```go
// PromptConfig holds the configuration for building a compressed prompt.
type PromptConfig struct {
    // Level controls compression aggressiveness.
    Level CompressionLevel

    // GraphContext is a list of graph nodes relevant to the current task.
    // These get converted to shorthand and injected into the prompt.
    // Can be nil if no graph context is available.
    GraphContext []GraphNodeInfo

    // OutputSchema is the JSON schema string for structured output.
    // Only used when Level is LevelFull. Can be empty.
    OutputSchema string

    // TaskType categorizes the task for format selection.
    // Used with LevelFull to pick the right JSON schema.
    TaskType TaskType
}

// TaskType identifies what kind of output the agent should produce.
// This determines which JSON schema is used in LevelFull mode.
type TaskType string

const (
    TaskFix      TaskType = "fix"       // Code fix generation
    TaskTest     TaskType = "test"      // Test generation
    TaskPR       TaskType = "pr"        // PR description
    TaskAnalysis TaskType = "analysis"  // Impact analysis
    TaskGeneral  TaskType = "general"   // General response (no specific schema)
)

// BuildPrompt constructs the full prompt by combining:
// 1. The compression system prompt (based on level)
// 2. Graph shorthand context (if graph nodes provided)
// 3. Output schema (if level is full)
// 4. The original user/task prompt
//
// Parameters:
//   - basePrompt: the original task prompt or user message
//   - config: compression configuration
//
// Returns: the complete prompt string ready to send to the LLM
//
// Example usage:
//
//   prompt := compress.BuildPrompt("Fix the type mismatch in auth.validate()", compress.PromptConfig{
//       Level: compress.LevelCompact,
//       GraphContext: graphNodes,
//   })
//
func BuildPrompt(basePrompt string, config PromptConfig) string {
    // Implementation steps:
    // 1. Select the compression prompt based on config.Level
    // 2. If config.GraphContext is not nil and not empty:
    //    a. Call BuildShorthand(config.GraphContext) from shorthand.go
    //    b. Append "GRAPH CONTEXT (use as shorthand):\n" + shorthand
    // 3. If config.Level is LevelFull:
    //    a. If config.OutputSchema is not empty, append it
    //    b. Else if config.TaskType is set, call GetOutputSchema(config.TaskType)
    //       from formatter.go and append it
    // 4. Append "\n\nTASK:\n" + basePrompt
    // 5. Return the assembled string
}

// EstimateTokenSavings returns the approximate token reduction percentage
// for a given compression level. Useful for cost tracking dashboards.
//
// Returns a float64 between 0.0 and 1.0 representing the fraction saved.
//   - LevelFull:    0.85 (85% — prose eliminated entirely)
//   - LevelCompact: 0.75 (75% — filler stripped, shorthand used)
//   - LevelNormal:  0.30 (30% — light cleanup only)
func EstimateTokenSavings(level CompressionLevel) float64 {
    // Simple lookup — these are approximate values from Caveman benchmarks
    // and LLMLingua research. Adjust based on real measurements.
}
```

---

## 5. File: `internal/compress/shorthand.go`

### Purpose
Converts verbose graph node descriptions into compact one-line references. This is the "context distillation" piece — same information, far fewer tokens.

### Types

```go
// GraphNodeInfo contains the graph data needed to generate shorthand.
// This struct should mirror the relevant fields from your existing
// models.Node type in internal/models/models.go.
//
// When integrating, you'll populate this from your graph query results.
type GraphNodeInfo struct {
    // ID is the unique node identifier from the graph.
    // Format: "repo:package:function_name" or "repo:package:type_name"
    ID string

    // Name is the function/type/variable name.
    Name string

    // Kind is the node type: "function", "struct", "interface", "method", "package", "variable"
    Kind string

    // Repo is the short repository name (e.g., "auth-service").
    Repo string

    // Package is the Go package name (e.g., "auth").
    Package string

    // File is the relative file path (e.g., "validate.go").
    File string

    // Line is the line number where the entity is defined.
    Line int

    // Exported indicates if the symbol is exported (starts with uppercase in Go).
    Exported bool

    // Callers is the list of node IDs that call/reference this node.
    // Populated from graph edge traversal.
    Callers []string

    // Callees is the list of node IDs that this node calls/references.
    // Populated from graph edge traversal.
    Callees []string

    // CallerNames is the list of compact names for callers (e.g., "gateway.LoginHandler").
    // Pre-resolved for shorthand generation so we don't need the full graph here.
    CallerNames []string

    // CalleeNames is the list of compact names for callees.
    CalleeNames []string
}
```

### Functions

```go
// BuildShorthand converts a list of graph nodes into a compact
// shorthand block that gets injected into the agent's prompt.
//
// The output format is one line per node:
//
//   • auth.ValidateToken [func] (validate.go:42) ← gateway.LoginHandler, token.RefreshToken
//   • auth.TokenPayload [struct] (models.go:15) → used by ValidateToken, RefreshToken
//   • gateway.LoginHandler [func] (handler.go:88) ← api.Router
//
// Format rules:
//   - Package.Name [kind] (file:line)
//   - ← callers (who calls this)
//   - → callees (what this calls)
//   - If callers > 5, show first 3 + "and N more"
//   - If callees > 5, show first 3 + "and N more"
//   - Skip callers/callees section if empty
//
// Parameters:
//   - nodes: list of graph nodes relevant to the current task
//
// Returns: formatted shorthand string ready for prompt injection
//
// Example output:
//   GRAPH CONTEXT (use as shorthand):
//   • auth.ValidateToken [func] (validate.go:42) ← gateway.LoginHandler, token.RefreshToken
//   • auth.TokenPayload [struct] (models.go:15)
//   • gateway.LoginHandler [func] (handler.go:88) ← api.Router → auth.ValidateToken, middleware.RateLimit
//
func BuildShorthand(nodes []GraphNodeInfo) string {
    // Implementation steps:
    // 1. Create a strings.Builder
    // 2. For each node:
    //    a. Write "• " + node.Package + "." + node.Name
    //    b. Write " [" + node.Kind + "]"
    //    c. Write " (" + node.File + ":" + strconv.Itoa(node.Line) + ")"
    //    d. If len(node.CallerNames) > 0:
    //       - Write " ← " + formatNameList(node.CallerNames, 5)
    //    e. If len(node.CalleeNames) > 0:
    //       - Write " → " + formatNameList(node.CalleeNames, 5)
    //    f. Write "\n"
    // 3. Return builder.String()
}

// BuildShorthandCompact generates an even more compressed version
// for use in LevelFull mode where every token counts.
//
// Format: one node per line, no bullets, no brackets:
//   auth.ValidateToken validate.go:42 ←2 →1
//   gateway.LoginHandler handler.go:88 ←1 →3
//
// The numbers after ← and → are caller/callee counts, not names.
// The agent can reference node names; the counts give it scope awareness.
//
func BuildShorthandCompact(nodes []GraphNodeInfo) string {
    // Implementation: same as BuildShorthand but:
    // - No bullet prefix
    // - No [kind]
    // - Callers/callees as counts only: ←N →N
    // - Skip if count is 0
}

// formatNameList joins names with ", " and truncates if over maxShow.
// e.g., formatNameList(["a", "b", "c", "d", "e", "f"], 3) → "a, b, c and 3 more"
func formatNameList(names []string, maxShow int) string {
    // Implementation:
    // If len(names) <= maxShow: return strings.Join(names, ", ")
    // Else: return strings.Join(names[:maxShow], ", ") + " and " + strconv.Itoa(len(names)-maxShow) + " more"
}

// NodeInfoFromGraph converts your existing graph Node type to GraphNodeInfo.
// This is the integration point between Engine 4 and your existing graph package.
//
// You will need to adapt this function to match your actual models.Node type
// defined in internal/models/models.go. The function should:
//
//   1. Copy basic fields (ID, Name, Kind, Repo, Package, File, Line, Exported)
//   2. Query the graph for incoming edges (callers) and outgoing edges (callees)
//   3. Resolve caller/callee IDs to compact "Package.Name" format
//
// Parameters:
//   - node: your existing graph node
//   - graph: your existing graph structure (for edge traversal)
//
// This function signature will need adjustment based on your actual types.
// Placeholder:
//
// func NodeInfoFromGraph(node models.Node, graph *graph.Graph) GraphNodeInfo { ... }
//
// NOTE: Do not implement this function yet. It will be implemented during
// MCP server integration when we wire Engine 4 to the live graph.
// For now, tests should construct GraphNodeInfo directly.
```

---

## 6. File: `internal/compress/formatter.go`

### Purpose
Provides JSON output schemas for machine-to-machine communication. When the agent's output feeds another system (test runner, PR creator, sandbox), we force structured JSON output instead of prose — eliminating all wasted tokens.

### Types and Constants

```go
// OutputSchema defines a JSON schema that the agent must follow
// when producing structured output in LevelFull mode.
type OutputSchema struct {
    // Name is the schema identifier (e.g., "fix", "test", "pr")
    Name string

    // Description tells the agent what this schema is for
    Description string

    // Schema is the JSON schema string that the agent must match
    Schema string

    // Example is a concrete example of valid output (helps the agent)
    Example string
}
```

### Schema Definitions

Define these as package-level variables:

```go
// FixOutputSchema — for code fix generation tasks.
// Used when the agent needs to produce a code change.
var FixOutputSchema = OutputSchema{
    Name:        "fix",
    Description: "Code fix specification",
    Schema: `{
  "fixes": [
    {
      "file": "relative/path/to/file.go",
      "line": 42,
      "old_code": "the exact code to replace",
      "new_code": "the replacement code",
      "reason": "one sentence explaining why"
    }
  ],
  "affected_nodes": ["repo:package:function"],
  "confidence": 0.95
}`,
    Example: `{
  "fixes": [
    {
      "file": "internal/auth/validate.go",
      "line": 42,
      "old_code": "func ValidateToken(token int) error {",
      "new_code": "func ValidateToken(token string) error {",
      "reason": "Token parameter should be string, not int — matches API contract"
    }
  ],
  "affected_nodes": ["auth-service:auth:ValidateToken"],
  "confidence": 0.92
}`,
}

// TestOutputSchema — for test generation tasks.
// Used when the agent needs to produce test code.
var TestOutputSchema = OutputSchema{
    Name:        "test",
    Description: "Generated test specification",
    Schema: `{
  "tests": [
    {
      "file": "relative/path/to/file_test.go",
      "function_name": "TestFunctionName",
      "content": "full test function code",
      "covers_node": "repo:package:function"
    }
  ],
  "test_command": "go test ./path/to/package -run TestName"
}`,
    Example: `{
  "tests": [
    {
      "file": "internal/auth/validate_test.go",
      "function_name": "TestValidateToken_StringInput",
      "content": "func TestValidateToken_StringInput(t *testing.T) {\n\terr := ValidateToken(\"abc123\")\n\tif err != nil {\n\t\tt.Fatalf(\"expected nil error, got %v\", err)\n\t}\n}",
      "covers_node": "auth-service:auth:ValidateToken"
    }
  ],
  "test_command": "go test ./internal/auth -run TestValidateToken_StringInput"
}`,
}

// PROutputSchema — for PR description generation.
var PROutputSchema = OutputSchema{
    Name:        "pr",
    Description: "Pull request metadata",
    Schema: `{
  "title": "short PR title",
  "body": "markdown PR description",
  "labels": ["bug", "cross-repo"],
  "reviewers": ["username"],
  "affected_repos": ["repo-name"]
}`,
    Example: `{
  "title": "fix: correct token type mismatch in auth.ValidateToken",
  "body": "## What changed\nChanged token parameter from int to string in ValidateToken to match the API contract defined in gateway-service.\n\n## Impact\n- auth-service: validate.go line 42\n- 3 callers updated in gateway-service\n\n## Testing\n- Added TestValidateToken_StringInput\n- All existing tests pass",
  "labels": ["bug", "cross-repo"],
  "reviewers": [],
  "affected_repos": ["auth-service", "gateway-service"]
}`,
}

// AnalysisOutputSchema — for impact analysis tasks.
var AnalysisOutputSchema = OutputSchema{
    Name:        "analysis",
    Description: "Impact analysis result",
    Schema: `{
  "root_cause": "one sentence",
  "affected_nodes": [
    {
      "node_id": "repo:package:function",
      "impact": "high|medium|low",
      "reason": "why this node is affected"
    }
  ],
  "suggested_fix": "brief fix description",
  "risk_level": "high|medium|low",
  "cross_repo": true
}`,
    Example: `{
  "root_cause": "Type mismatch — auth.ValidateToken expects int but gateway sends string",
  "affected_nodes": [
    {
      "node_id": "auth-service:auth:ValidateToken",
      "impact": "high",
      "reason": "Direct type mismatch on parameter"
    },
    {
      "node_id": "gateway-service:handlers:LoginHandler",
      "impact": "medium",
      "reason": "Caller — sends string token to ValidateToken"
    }
  ],
  "suggested_fix": "Change ValidateToken parameter from int to string",
  "risk_level": "medium",
  "cross_repo": true
}`,
}
```

### Functions

```go
// GetOutputSchema returns the appropriate schema for a given task type.
// Returns nil if the task type is TaskGeneral (no schema needed).
//
// Parameters:
//   - taskType: the type of task being performed
//
// Returns: pointer to OutputSchema, or nil for TaskGeneral
func GetOutputSchema(taskType TaskType) *OutputSchema {
    // Implementation: simple switch statement
    // TaskFix → &FixOutputSchema
    // TaskTest → &TestOutputSchema
    // TaskPR → &PROutputSchema
    // TaskAnalysis → &AnalysisOutputSchema
    // TaskGeneral → nil
}

// FormatSchemaPrompt converts an OutputSchema into a prompt string
// that instructs the agent to produce output matching the schema.
//
// The output includes:
// 1. The schema description
// 2. The JSON schema
// 3. A concrete example
// 4. Strict instructions to output ONLY valid JSON
//
// Parameters:
//   - schema: the output schema to format
//
// Returns: formatted prompt string
//
// Example return value:
//   OUTPUT SCHEMA — respond with JSON matching this structure:
//   {
//     "fixes": [ ... ]
//   }
//
//   EXAMPLE:
//   {
//     "fixes": [ ... ]
//   }
//
//   Respond with ONLY the JSON. No markdown fences. No explanation.
//
func FormatSchemaPrompt(schema *OutputSchema) string {
    // Implementation:
    // 1. Write "OUTPUT SCHEMA — " + schema.Description + ":\n"
    // 2. Write schema.Schema + "\n\n"
    // 3. Write "EXAMPLE:\n" + schema.Example + "\n\n"
    // 4. Write "Respond with ONLY the JSON. No markdown fences. No explanation. No preamble."
}

// ParseFixOutput parses the agent's JSON response into a typed struct.
// Use this when processing LevelFull output for fix tasks.
//
// Returns error if the JSON is malformed or doesn't match the schema.
type FixOutput struct {
    Fixes []struct {
        File     string `json:"file"`
        Line     int    `json:"line"`
        OldCode  string `json:"old_code"`
        NewCode  string `json:"new_code"`
        Reason   string `json:"reason"`
    } `json:"fixes"`
    AffectedNodes []string `json:"affected_nodes"`
    Confidence    float64  `json:"confidence"`
}

func ParseFixOutput(jsonStr string) (*FixOutput, error) {
    // Implementation:
    // 1. Strip markdown code fences if present (```json ... ```)
    //    — agents sometimes add them despite instructions
    // 2. strings.TrimSpace
    // 3. json.Unmarshal into FixOutput
    // 4. Validate required fields are present
    // 5. Return parsed struct or error
}

// ParseTestOutput parses test generation JSON output.
type TestOutput struct {
    Tests []struct {
        File         string `json:"file"`
        FunctionName string `json:"function_name"`
        Content      string `json:"content"`
        CoversNode   string `json:"covers_node"`
    } `json:"tests"`
    TestCommand string `json:"test_command"`
}

func ParseTestOutput(jsonStr string) (*TestOutput, error) {
    // Same pattern as ParseFixOutput
}

// ParsePROutput parses PR generation JSON output.
type PROutput struct {
    Title         string   `json:"title"`
    Body          string   `json:"body"`
    Labels        []string `json:"labels"`
    Reviewers     []string `json:"reviewers"`
    AffectedRepos []string `json:"affected_repos"`
}

func ParsePROutput(jsonStr string) (*PROutput, error) {
    // Same pattern as ParseFixOutput
}

// ParseAnalysisOutput parses impact analysis JSON output.
type AnalysisOutput struct {
    RootCause     string `json:"root_cause"`
    AffectedNodes []struct {
        NodeID string `json:"node_id"`
        Impact string `json:"impact"`
        Reason string `json:"reason"`
    } `json:"affected_nodes"`
    SuggestedFix string `json:"suggested_fix"`
    RiskLevel    string `json:"risk_level"`
    CrossRepo    bool   `json:"cross_repo"`
}

func ParseAnalysisOutput(jsonStr string) (*AnalysisOutput, error) {
    // Same pattern as ParseFixOutput
}

// stripCodeFences removes markdown code fences from LLM output.
// Agents sometimes wrap JSON in ```json ... ``` despite being told not to.
// This helper ensures clean parsing regardless.
func stripCodeFences(s string) string {
    // Implementation:
    // 1. strings.TrimSpace(s)
    // 2. If strings.HasPrefix(s, "```json"), strip first line
    // 3. If strings.HasPrefix(s, "```"), strip first line
    // 4. If strings.HasSuffix(s, "```"), strip last line
    // 5. Return strings.TrimSpace(result)
}
```

---

## 7. How BuildPrompt Assembles Everything

Here's the exact flow when `BuildPrompt` is called:

### Example 1: Compact mode (developer asks "fix the type mismatch")

```
Input:
  basePrompt: "Fix the type mismatch in auth.ValidateToken that's causing gateway failures"
  config.Level: LevelCompact
  config.GraphContext: [ValidateToken node, LoginHandler node]

Output (what gets sent to the LLM):
  ┌─────────────────────────────────────────────────────────────────┐
  │ COMMUNICATION RULES — FOLLOW STRICTLY:                         │
  │ - Drop articles (a, an, the) from explanations                 │
  │ - No pleasantries...                                           │
  │ [full compactPrompt]                                           │
  │                                                                │
  │ GRAPH CONTEXT (use as shorthand):                              │
  │ • auth.ValidateToken [func] (validate.go:42) ← gateway.Login  │
  │   Handler, token.RefreshToken                                  │
  │ • gateway.LoginHandler [func] (handler.go:88) ← api.Router    │
  │   → auth.ValidateToken, middleware.RateLimit                   │
  │                                                                │
  │ TASK:                                                          │
  │ Fix the type mismatch in auth.ValidateToken that's causing     │
  │ gateway failures                                               │
  └─────────────────────────────────────────────────────────────────┘
```

### Example 2: Full mode (agent output feeds the test runner)

```
Input:
  basePrompt: "Generate tests for the ValidateToken fix"
  config.Level: LevelFull
  config.TaskType: TaskTest
  config.GraphContext: [ValidateToken node]

Output:
  ┌─────────────────────────────────────────────────────────────────┐
  │ OUTPUT FORMAT — STRICT:                                        │
  │ - Respond ONLY with valid JSON matching the provided schema    │
  │ [full fullPrompt]                                              │
  │                                                                │
  │ GRAPH CONTEXT:                                                 │
  │ auth.ValidateToken validate.go:42 ←2 →1                       │
  │                                                                │
  │ OUTPUT SCHEMA — Generated test specification:                  │
  │ {                                                              │
  │   "tests": [ ... ]                                             │
  │ }                                                              │
  │                                                                │
  │ EXAMPLE:                                                       │
  │ { ... }                                                        │
  │                                                                │
  │ Respond with ONLY the JSON. No markdown fences. No explanation.│
  │                                                                │
  │ TASK:                                                          │
  │ Generate tests for the ValidateToken fix                       │
  └─────────────────────────────────────────────────────────────────┘
```

### Example 3: Normal mode (developer wants an explanation)

```
Input:
  basePrompt: "Explain what will break if I change the token type"
  config.Level: LevelNormal
  config.GraphContext: [ValidateToken node, LoginHandler node, RefreshToken node]

Output:
  ┌─────────────────────────────────────────────────────────────────┐
  │ COMMUNICATION RULES:                                           │
  │ - Do not start responses with "I'd be happy to help"...        │
  │ [full normalPrompt]                                            │
  │                                                                │
  │ GRAPH CONTEXT (use as shorthand):                              │
  │ • auth.ValidateToken [func] (validate.go:42) ← gateway.Login  │
  │   Handler, token.RefreshToken                                  │
  │ • gateway.LoginHandler [func] (handler.go:88) ← api.Router    │
  │ • token.RefreshToken [func] (refresh.go:21) → auth.Validate   │
  │   Token                                                        │
  │                                                                │
  │ TASK:                                                          │
  │ Explain what will break if I change the token type             │
  └─────────────────────────────────────────────────────────────────┘
```

---

## 8. Integration Points

### 8.1 How Engine 4 connects to the existing graph

Engine 4 needs graph node data to generate shorthand. The connection point is a function that converts your existing `models.Node` and graph edges into `GraphNodeInfo`.

**Do NOT modify your existing graph package now.** Instead, create the integration when you build the MCP server or agent pipeline. For now, Engine 4 works with `GraphNodeInfo` structs that can be constructed manually or from test data.

The future integration will look like:

```go
// In your future MCP server or agent pipeline code:
import (
    "universe/internal/compress"
    "universe/internal/graph"
    "universe/internal/models"
)

func handleAgentTask(task string, relevantNodes []models.Node, g *graph.Graph) string {
    // Convert graph nodes to compress-compatible format
    nodeInfos := make([]compress.GraphNodeInfo, len(relevantNodes))
    for i, n := range relevantNodes {
        nodeInfos[i] = compress.GraphNodeInfo{
            ID:          n.ID,
            Name:        n.Name,
            Kind:        string(n.Type),  // adjust to match your models.Node fields
            Package:     n.Package,
            File:        n.FilePath,
            Line:        n.Line,
            CallerNames: getCallerNames(g, n.ID),  // resolve from graph edges
            CalleeNames: getCalleeNames(g, n.ID),  // resolve from graph edges
        }
    }

    // Build the compressed prompt
    prompt := compress.BuildPrompt(task, compress.PromptConfig{
        Level:        compress.LevelCompact,
        GraphContext: nodeInfos,
    })

    // Send prompt to LLM...
    return callLLM(prompt)
}
```

### 8.2 How Engine 4 connects to future engines

| Future engine | How it uses Engine 4 |
|---------------|---------------------|
| Engine 2 (Memory) | Memory retrieval results get compressed via `BuildShorthand` before injection into prompt — keeps memory context token-efficient |
| Engine 3 (Skills) | Skill instructions are already compressed (they're written by the evolver). But the agent's output when executing a skill uses the compression prompt |
| Engine 5 (Orchestrator) | Every LLM call goes through `BuildPrompt`. Planner calls use `LevelCompact`. Executor calls that feed machine consumers use `LevelFull`. Verifier calls use `LevelNormal` |

### 8.3 How Engine 4 connects to MCP

When the MCP server is built, it will expose a tool for configuring compression:

```json
{
  "set_compression": {
    "description": "Set the compression level for agent responses",
    "input": {
      "level": "full | compact | normal"
    }
  }
}
```

The MCP server stores the active compression level per session and passes it to `BuildPrompt` for every agent call. Default is `LevelCompact`.

---

## 9. Testing Strategy

### Unit tests to write

Create `internal/compress/compress_test.go`:

```go
package compress

import "testing"

// Test 1: BuildShorthand produces correct format
func TestBuildShorthand_BasicNode(t *testing.T) {
    nodes := []GraphNodeInfo{
        {
            Name:        "ValidateToken",
            Kind:        "function",
            Package:     "auth",
            File:        "validate.go",
            Line:        42,
            CallerNames: []string{"gateway.LoginHandler", "token.RefreshToken"},
            CalleeNames: []string{"crypto.VerifyJWT"},
        },
    }
    result := BuildShorthand(nodes)
    // Should contain: "• auth.ValidateToken [function] (validate.go:42)"
    // Should contain: "← gateway.LoginHandler, token.RefreshToken"
    // Should contain: "→ crypto.VerifyJWT"
}

// Test 2: BuildShorthand truncates long caller lists
func TestBuildShorthand_TruncatesCallers(t *testing.T) {
    node := GraphNodeInfo{
        Name:        "Serialize",
        Kind:        "function",
        Package:     "utils",
        File:        "json.go",
        Line:        10,
        CallerNames: []string{"a.One", "b.Two", "c.Three", "d.Four", "e.Five", "f.Six", "g.Seven"},
    }
    result := BuildShorthand([]GraphNodeInfo{node})
    // Should contain: "← a.One, b.Two, c.Three, d.Four, e.Five and 2 more"
}

// Test 3: BuildShorthandCompact produces minimal format
func TestBuildShorthandCompact_MinimalFormat(t *testing.T) {
    nodes := []GraphNodeInfo{
        {
            Name:        "ValidateToken",
            Package:     "auth",
            File:        "validate.go",
            Line:        42,
            CallerNames: []string{"a", "b"},
            CalleeNames: []string{"c"},
        },
    }
    result := BuildShorthandCompact(nodes)
    // Should be: "auth.ValidateToken validate.go:42 ←2 →1\n"
    // No bullets, no [kind], just counts
}

// Test 4: BuildPrompt assembles correctly for each level
func TestBuildPrompt_CompactLevel(t *testing.T) {
    prompt := BuildPrompt("Fix the bug", PromptConfig{
        Level: LevelCompact,
        GraphContext: []GraphNodeInfo{
            {Name: "Foo", Kind: "function", Package: "bar", File: "baz.go", Line: 1},
        },
    })
    // Should contain compactPrompt text
    // Should contain "GRAPH CONTEXT"
    // Should contain "bar.Foo"
    // Should contain "TASK:\nFix the bug"
    // Should NOT contain any JSON schema
}

// Test 5: BuildPrompt with LevelFull includes schema
func TestBuildPrompt_FullLevelWithTaskType(t *testing.T) {
    prompt := BuildPrompt("Generate tests", PromptConfig{
        Level:    LevelFull,
        TaskType: TaskTest,
    })
    // Should contain fullPrompt text
    // Should contain TestOutputSchema.Schema
    // Should contain TestOutputSchema.Example
    // Should contain "TASK:\nGenerate tests"
}

// Test 6: ParseFixOutput handles clean JSON
func TestParseFixOutput_ValidJSON(t *testing.T) {
    input := `{"fixes":[{"file":"a.go","line":1,"old_code":"old","new_code":"new","reason":"why"}],"affected_nodes":["repo:pkg:fn"],"confidence":0.9}`
    result, err := ParseFixOutput(input)
    // err should be nil
    // result.Fixes should have 1 entry
    // result.Confidence should be 0.9
}

// Test 7: ParseFixOutput strips markdown code fences
func TestParseFixOutput_WithCodeFences(t *testing.T) {
    input := "```json\n{\"fixes\":[],\"affected_nodes\":[],\"confidence\":0.5}\n```"
    result, err := ParseFixOutput(input)
    // err should be nil — code fences should be stripped
    // result.Confidence should be 0.5
}

// Test 8: ParseFixOutput returns error on invalid JSON
func TestParseFixOutput_InvalidJSON(t *testing.T) {
    _, err := ParseFixOutput("this is not json")
    // err should not be nil
}

// Test 9: formatNameList works correctly
func TestFormatNameList_UnderLimit(t *testing.T) {
    result := formatNameList([]string{"a.Foo", "b.Bar"}, 5)
    // Should be "a.Foo, b.Bar"
}

func TestFormatNameList_OverLimit(t *testing.T) {
    result := formatNameList([]string{"a", "b", "c", "d", "e", "f"}, 3)
    // Should be "a, b, c and 3 more"
}

// Test 10: EstimateTokenSavings returns expected values
func TestEstimateTokenSavings(t *testing.T) {
    // LevelFull should return ~0.85
    // LevelCompact should return ~0.75
    // LevelNormal should return ~0.30
}

// Test 11: Empty graph context — no graph section in prompt
func TestBuildPrompt_NoGraphContext(t *testing.T) {
    prompt := BuildPrompt("Do something", PromptConfig{
        Level: LevelCompact,
    })
    // Should contain compactPrompt
    // Should NOT contain "GRAPH CONTEXT"
    // Should contain "TASK:\nDo something"
}

// Test 12: GetOutputSchema returns correct schema
func TestGetOutputSchema(t *testing.T) {
    // TaskFix → FixOutputSchema
    // TaskTest → TestOutputSchema
    // TaskPR → PROutputSchema
    // TaskAnalysis → AnalysisOutputSchema
    // TaskGeneral → nil
}
```

### How to run tests

```bash
cd /path/to/universe
go test ./internal/compress/ -v
```

---

## 10. Acceptance Criteria

Engine 4 is complete when:

- [ ] `compress.BuildPrompt()` correctly assembles prompts for all three compression levels
- [ ] `compress.BuildShorthand()` converts graph nodes into readable one-line references
- [ ] `compress.BuildShorthandCompact()` produces minimal format for LevelFull mode
- [ ] `compress.GetOutputSchema()` returns the correct schema for each task type
- [ ] `compress.FormatSchemaPrompt()` produces a clear schema instruction block
- [ ] `compress.ParseFixOutput()`, `ParseTestOutput()`, `ParsePROutput()`, `ParseAnalysisOutput()` all correctly parse valid JSON and handle code fences
- [ ] All parse functions return clear errors on invalid input
- [ ] All 12 unit tests pass
- [ ] No changes to existing files in the project
- [ ] `go build ./...` succeeds with the new package added

---

## 11. What NOT to Build in This Engine

- Do NOT modify any existing files (graph.go, models.go, analyzer.go, etc.)
- Do NOT create database tables — this engine has no persistence
- Do NOT build the MCP integration — that comes later
- Do NOT build the Anthropic API client — that's Engine 5
- Do NOT implement `NodeInfoFromGraph()` — that's an integration step for later
- Do NOT add any external dependencies beyond the Go standard library — this package uses only `strings`, `strconv`, `fmt`, `encoding/json`

---

## 12. Future Improvements (Not in This Build)

These will be added in later iterations:

1. **Adaptive compression** — track actual token savings per compression level per developer and auto-adjust the default level
2. **Custom shorthand aliases** — let developers define shorthand for frequently referenced nodes (e.g., `auth.VT` for `auth.ValidateToken`)
3. **Input compression** — compress the incoming prompt before sending (currently we only compress output). Requires token counting which needs a tokenizer library
4. **Compression metrics endpoint** — expose token savings data for the cost dashboard (Engine 5 tracker will consume this)
