# CLI + Dashboard + npm — Alignment Patch

## Apply These Changes to cli-wiring.md, dashboard.md, dashboard-build.md, and npm-setup.md

---

# PART 1: CLI WIRING (cli-wiring.md) — 7 Changes

---

## Change 1: Add `universe setup` command

**ADD new file: `cmd/universe/setup.go`**

```go
package main

import (
    "bufio"
    "fmt"
    "os"
    "strconv"
    "strings"

    "github.com/spf13/cobra"

    "universe/internal/orchestrator"
)

var setupCmd = &cobra.Command{
    Use:   "setup",
    Short: "Configure Universe — pick models, generate workspace files and Cursor rules",
    Long: `Interactive setup that configures:
  - Your premium model (for planning and verification)
  - Your execution model (for coding and testing)
  - Cursor workspace files (auto-sets model per window)
  - Cursor rules (planner, executor, compression)
  - MCP connection config

Run this once per project. Change models later with:
  universe config set premium_model <name>`,
    Run: runSetup,
}

// Non-interactive flags
var setupPremium string
var setupExecution string
var setupDB string

func init() {
    setupCmd.Flags().StringVar(&setupPremium, "premium", "", "Premium model name (skip interactive)")
    setupCmd.Flags().StringVar(&setupExecution, "execution", "", "Execution model name (skip interactive)")
    setupCmd.Flags().StringVar(&setupDB, "db", "", "Database URL (skip interactive)")
    rootCmd.AddCommand(setupCmd)
}

// Model presets with pricing
var modelPresets = []struct {
    Name           string
    Provider       string
    InputCostPerM  float64
    OutputCostPerM float64
    Tier           string // "premium" or "execution" or "both"
}{
    {"claude-opus-4", "anthropic", 15.0, 75.0, "premium"},
    {"gpt-4o", "openai", 5.0, 15.0, "premium"},
    {"gemini-2.5-pro", "google", 1.25, 10.0, "premium"},
    {"o3", "openai", 10.0, 40.0, "premium"},
    {"claude-sonnet-4", "anthropic", 3.0, 15.0, "both"},
    {"claude-haiku-3.5", "anthropic", 0.25, 1.25, "execution"},
    {"gpt-4o-mini", "openai", 0.15, 0.60, "execution"},
    {"gemini-2.5-flash", "google", 0.15, 0.60, "execution"},
}

func runSetup(cmd *cobra.Command, args []string) {
    fmt.Println("🌌 Universe Setup")
    fmt.Println("═══════════════════════════════════════════════")
    fmt.Println()

    reader := bufio.NewReader(os.Stdin)

    // ── Step 1: Pick premium model ──
    var premiumModel orchestrator.ModelConfig

    if setupPremium != "" {
        premiumModel = findModelPreset(setupPremium)
    } else {
        fmt.Println("Choose your PREMIUM model (for planning & verification):")
        fmt.Println()
        premiumOptions := filterModels("premium")
        for i, m := range premiumOptions {
            fmt.Printf("  %d. %-25s (%s) — $%.1f/$%.1f per 1M tokens\n",
                i+1, m.Name, m.Provider, m.InputCostPerM, m.OutputCostPerM)
        }
        fmt.Printf("  %d. Custom (type model name)\n", len(premiumOptions)+1)
        fmt.Println()
        fmt.Print("> ")
        choice, _ := reader.ReadString('\n')
        choice = strings.TrimSpace(choice)

        idx, err := strconv.Atoi(choice)
        if err == nil && idx >= 1 && idx <= len(premiumOptions) {
            p := premiumOptions[idx-1]
            premiumModel = orchestrator.ModelConfig{
                Name: p.Name, Provider: p.Provider,
                InputCostPerM: p.InputCostPerM, OutputCostPerM: p.OutputCostPerM,
            }
        } else {
            // Custom model — ask for name and pricing
            fmt.Print("Model name: ")
            name, _ := reader.ReadString('\n')
            premiumModel = orchestrator.ModelConfig{
                Name: strings.TrimSpace(name), Provider: "custom",
                InputCostPerM: 10.0, OutputCostPerM: 30.0,
            }
        }
    }
    fmt.Printf("  ✓ Premium: %s\n\n", premiumModel.Name)

    // ── Step 2: Pick execution model ──
    var executionModel orchestrator.ModelConfig

    if setupExecution != "" {
        executionModel = findModelPreset(setupExecution)
    } else {
        fmt.Println("Choose your EXECUTION model (for coding & testing):")
        fmt.Println()
        execOptions := filterModels("execution")
        for i, m := range execOptions {
            fmt.Printf("  %d. %-25s (%s) — $%.2f/$%.2f per 1M tokens\n",
                i+1, m.Name, m.Provider, m.InputCostPerM, m.OutputCostPerM)
        }
        fmt.Printf("  %d. Custom (type model name)\n", len(execOptions)+1)
        fmt.Println()
        fmt.Print("> ")
        choice, _ := reader.ReadString('\n')
        choice = strings.TrimSpace(choice)

        idx, err := strconv.Atoi(choice)
        if err == nil && idx >= 1 && idx <= len(execOptions) {
            p := execOptions[idx-1]
            executionModel = orchestrator.ModelConfig{
                Name: p.Name, Provider: p.Provider,
                InputCostPerM: p.InputCostPerM, OutputCostPerM: p.OutputCostPerM,
            }
        } else {
            fmt.Print("Model name: ")
            name, _ := reader.ReadString('\n')
            executionModel = orchestrator.ModelConfig{
                Name: strings.TrimSpace(name), Provider: "custom",
                InputCostPerM: 0.25, OutputCostPerM: 1.25,
            }
        }
    }
    fmt.Printf("  ✓ Execution: %s\n\n", executionModel.Name)

    // ── Step 3: Database URL ──
    dbURL := setupDB
    if dbURL == "" {
        dbURL = GetDBURL()
    }
    if dbURL == "" {
        fmt.Println("Database URL (leave empty to skip — can configure later):")
        fmt.Print("> ")
        input, _ := reader.ReadString('\n')
        dbURL = strings.TrimSpace(input)
    }

    // ── Step 4: Save config ──
    cfg := LoadConfig()
    cfg.DBURL = dbURL
    cfg.Mode = "local"
    if dbURL != "" {
        cfg.Mode = "team"
    }
    cfg.PremiumModel = premiumModel
    cfg.ExecutionModel = executionModel
    SaveConfig(cfg)

    // ── Step 5: Generate all files ──
    projectDir, _ := os.Getwd()

    err := orchestrator.RunSetup(
        projectDir,
        premiumModel.Name,
        executionModel.Name,
        premiumModel,
        executionModel,
        dbURL,
    )
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error during setup: %v\n", err)
        os.Exit(1)
    }

    // ── Step 6: Print summary ──
    fmt.Println("═══════════════════════════════════════════════")
    fmt.Println("✅ Setup complete!")
    fmt.Println()
    fmt.Println("Created:")
    fmt.Println("  ~/.universe/config.json                        (model preferences)")
    fmt.Println("  .universe/workspaces/planner.code-workspace    (opens with " + premiumModel.Name + ")")
    fmt.Println("  .universe/workspaces/executor.code-workspace   (opens with " + executionModel.Name + ")")
    fmt.Println("  .cursor/rules/universe-planner.mdc             (planning agent rules)")
    fmt.Println("  .cursor/rules/universe-executor.mdc            (execution agent rules)")
    fmt.Println("  .cursor/rules/universe-compression.mdc         (caveman output mode)")
    fmt.Println("  .cursor/mcp.json                               (MCP server connection)")
    fmt.Println("  .vscode/settings.json                          (default model: " + executionModel.Name + ")")
    fmt.Println()
    fmt.Println("Quick start:")
    fmt.Println("  universe plan      Open planner window (" + premiumModel.Name + ")")
    fmt.Println("  universe exec      Open executor window (" + executionModel.Name + ")")
    fmt.Println("  universe start     Open both windows")
    fmt.Println()
    if dbURL != "" {
        fmt.Println("  universe db status   Test database connection")
    } else {
        fmt.Println("  universe config set db postgres://...   Connect database later")
    }
}

func findModelPreset(name string) orchestrator.ModelConfig {
    for _, p := range modelPresets {
        if p.Name == name {
            return orchestrator.ModelConfig{
                Name: p.Name, Provider: p.Provider,
                InputCostPerM: p.InputCostPerM, OutputCostPerM: p.OutputCostPerM,
            }
        }
    }
    return orchestrator.ModelConfig{Name: name, Provider: "custom", InputCostPerM: 5.0, OutputCostPerM: 15.0}
}

func filterModels(tier string) []struct {
    Name string; Provider string; InputCostPerM float64; OutputCostPerM float64; Tier string
} {
    var result []struct {
        Name string; Provider string; InputCostPerM float64; OutputCostPerM float64; Tier string
    }
    for _, m := range modelPresets {
        if m.Tier == tier || m.Tier == "both" {
            result = append(result, m)
        }
    }
    return result
}
```

---

## Change 2: Add `universe plan`, `universe exec`, `universe start` commands

**ADD new file: `cmd/universe/workspace.go`**

```go
package main

import (
    "fmt"
    "os"
    "path/filepath"

    "github.com/spf13/cobra"

    "universe/internal/orchestrator"
)

var planCmd = &cobra.Command{
    Use:   "plan",
    Short: "Open the planner Cursor window (premium model)",
    Long:  "Opens a Cursor window configured with your premium model for planning and verification.",
    Run:   runPlan,
}

var execCmd = &cobra.Command{
    Use:   "exec",
    Short: "Open the executor Cursor window (execution model)",
    Long:  "Opens a Cursor window configured with your execution model for coding and testing.",
    Run:   runExec,
}

var startCmd = &cobra.Command{
    Use:   "start",
    Short: "Open both planner and executor Cursor windows",
    Long:  "Opens two Cursor windows side by side — planner (premium) and executor (cheap).",
    Run:   runStart,
}

func init() {
    rootCmd.AddCommand(planCmd)
    rootCmd.AddCommand(execCmd)
    rootCmd.AddCommand(startCmd)
}

func runPlan(cmd *cobra.Command, args []string) {
    projectDir, _ := os.Getwd()
    wsPath := filepath.Join(projectDir, ".universe", "workspaces", "planner.code-workspace")

    if _, err := os.Stat(wsPath); os.IsNotExist(err) {
        fmt.Println("❌ Planner workspace not found.")
        fmt.Println("   Run 'universe setup' first to generate workspace files.")
        os.Exit(1)
    }

    cfg := LoadConfig()
    fmt.Printf("🧠 Opening planner window (%s)...\n", cfg.PremiumModel.Name)

    if err := orchestrator.OpenPlannerWorkspace(projectDir); err != nil {
        fmt.Fprintf(os.Stderr, "Error opening Cursor: %v\n", err)
        fmt.Println("   Manual: cursor " + wsPath)
        os.Exit(1)
    }
}

func runExec(cmd *cobra.Command, args []string) {
    projectDir, _ := os.Getwd()
    wsPath := filepath.Join(projectDir, ".universe", "workspaces", "executor.code-workspace")

    if _, err := os.Stat(wsPath); os.IsNotExist(err) {
        fmt.Println("❌ Executor workspace not found.")
        fmt.Println("   Run 'universe setup' first to generate workspace files.")
        os.Exit(1)
    }

    cfg := LoadConfig()
    fmt.Printf("⚡ Opening executor window (%s)...\n", cfg.ExecutionModel.Name)

    if err := orchestrator.OpenExecutorWorkspace(projectDir); err != nil {
        fmt.Fprintf(os.Stderr, "Error opening Cursor: %v\n", err)
        fmt.Println("   Manual: cursor " + wsPath)
        os.Exit(1)
    }
}

func runStart(cmd *cobra.Command, args []string) {
    projectDir, _ := os.Getwd()
    cfg := LoadConfig()

    fmt.Println("🌌 Opening Universe workspaces...")
    fmt.Printf("   🧠 Planner: %s\n", cfg.PremiumModel.Name)
    fmt.Printf("   ⚡ Executor: %s\n", cfg.ExecutionModel.Name)
    fmt.Println()

    // Open planner first, small delay, then executor
    if err := orchestrator.OpenPlannerWorkspace(projectDir); err != nil {
        fmt.Fprintf(os.Stderr, "   Warning: couldn't open planner: %v\n", err)
    }

    // Brief pause so Cursor doesn't merge them into one window
    // time.Sleep(1 * time.Second)

    if err := orchestrator.OpenExecutorWorkspace(projectDir); err != nil {
        fmt.Fprintf(os.Stderr, "   Warning: couldn't open executor: %v\n", err)
    }

    fmt.Println("Both windows should be open.")
    fmt.Println()
    fmt.Println("Workflow:")
    fmt.Println("  1. In 🧠 Planner: describe the task")
    fmt.Println("  2. In ⚡ Executor: say 'execute'")
    fmt.Println("  3. In 🧠 Planner: say 'verify'")
}
```

---

## Change 3: Add `universe setup-rules` command

**ADD to `cmd/universe/setup.go`:**

```go
var setupRulesCmd = &cobra.Command{
    Use:   "setup-rules",
    Short: "Regenerate Cursor rules files only (without full setup)",
    Long:  "Regenerates .cursor/rules/ files using your current model configuration.",
    Run:   runSetupRules,
}

func init() {
    // ... existing setup init ...
    rootCmd.AddCommand(setupRulesCmd)
}

func runSetupRules(cmd *cobra.Command, args []string) {
    cfg := LoadConfig()

    if cfg.PremiumModel.Name == "" || cfg.ExecutionModel.Name == "" {
        fmt.Println("❌ No models configured. Run 'universe setup' first.")
        os.Exit(1)
    }

    projectDir, _ := os.Getwd()
    orchestrator.GenerateCursorRules(projectDir, cfg.PremiumModel.Name, cfg.ExecutionModel.Name)

    fmt.Println("✅ Cursor rules regenerated:")
    fmt.Println("  .cursor/rules/universe-planner.mdc")
    fmt.Println("  .cursor/rules/universe-executor.mdc")
    fmt.Println("  .cursor/rules/universe-compression.mdc")
}
```

---

## Change 4: Update `UniverseConfig` in helpers.go

**ADD model fields to the config struct:**

```go
// BEFORE:
type UniverseConfig struct {
    DBURL string `json:"db_url"`
    Mode  string `json:"mode"`
}

// AFTER:
type UniverseConfig struct {
    DBURL          string                     `json:"db_url"`
    Mode           string                     `json:"mode"`
    DeveloperID    string                     `json:"developer_id,omitempty"`
    PremiumModel   orchestrator.ModelConfig   `json:"premium_model"`
    ExecutionModel orchestrator.ModelConfig   `json:"execution_model"`
}
```

---

## Change 5: Update `universe config set` to support model keys

**In cmd/universe/config.go, add model config cases to runConfigSet:**

```go
// ADD these cases inside the switch statement in runConfigSet:

case "premium_model":
    cfg := LoadConfig()
    cfg.PremiumModel = findModelPreset(value)
    SaveConfig(cfg)
    // Regenerate workspace file with new model
    projectDir, _ := os.Getwd()
    orchestrator.GenerateWorkspaces(projectDir, cfg.PremiumModel.Name, cfg.ExecutionModel.Name)
    fmt.Printf("✅ Premium model set to: %s\n", value)
    fmt.Println("   Workspace files regenerated.")

case "execution_model":
    cfg := LoadConfig()
    cfg.ExecutionModel = findModelPreset(value)
    SaveConfig(cfg)
    projectDir, _ := os.Getwd()
    orchestrator.GenerateWorkspaces(projectDir, cfg.PremiumModel.Name, cfg.ExecutionModel.Name)
    // Also update .vscode/settings.json default
    orchestrator.SetDefaultModel(projectDir, value)
    fmt.Printf("✅ Execution model set to: %s\n", value)
    fmt.Println("   Workspace files and default model regenerated.")

case "developer_id":
    cfg := LoadConfig()
    cfg.DeveloperID = value
    SaveConfig(cfg)
    fmt.Printf("✅ Developer ID set to: %s\n", value)
```

**Also update `universe config get` to show model config:**

```go
// ADD to runConfigGet:

case "models":
    cfg := LoadConfig()
    fmt.Println("Model configuration:")
    fmt.Printf("  Premium:   %s (%s) — $%.1f/$%.1f per 1M tokens\n",
        cfg.PremiumModel.Name, cfg.PremiumModel.Provider,
        cfg.PremiumModel.InputCostPerM, cfg.PremiumModel.OutputCostPerM)
    fmt.Printf("  Execution: %s (%s) — $%.2f/$%.2f per 1M tokens\n",
        cfg.ExecutionModel.Name, cfg.ExecutionModel.Provider,
        cfg.ExecutionModel.InputCostPerM, cfg.ExecutionModel.OutputCostPerM)
```

---

## Change 6: Update `universe status` to show models

**In cmd/universe/status.go, add model info to the footer:**

```go
// BEFORE:
func printStatusFooter(mode string, dbURL string) {
    fmt.Println("  Mode:     ...")
    fmt.Println("  Database: ...")
    fmt.Println("  Platform: ...")
    fmt.Println("  Version:  ...")
}

// AFTER:
func printStatusFooter(mode string, dbURL string) {
    cfg := LoadConfig()
    fmt.Println("═══════════════════════════════════════════════")
    if mode == "local" {
        fmt.Println("  Mode:      local (SQLite)")
        fmt.Println("  Database:  not connected")
    } else {
        fmt.Println("  Mode:      team (PostgreSQL)")
        fmt.Printf("  Database:  %s\n", MaskPassword(dbURL))
    }
    if cfg.PremiumModel.Name != "" {
        fmt.Printf("  Premium:   %s\n", cfg.PremiumModel.Name)
        fmt.Printf("  Execution: %s\n", cfg.ExecutionModel.Name)
    } else {
        fmt.Println("  Models:    not configured (run 'universe setup')")
    }
    fmt.Printf("  Platform:  %s\n", getPlatform())
    fmt.Printf("  Version:   %s\n", Version)
}
```

---

## Change 7: Update command tree

**The complete command tree after all changes:**

```
universe                              Show help
universe --version                    Print version

universe init                         Scan codebase, build graph
universe init --path /other/project   Scan a specific directory

universe setup                        Interactive model picker + generate all config files
universe setup --premium "gpt-4o" --execution "gpt-4o-mini"   Non-interactive
universe setup-rules                  Regenerate Cursor rules only

universe plan                         Open planner Cursor window (premium model)
universe exec                         Open executor Cursor window (execution model)
universe start                        Open both windows

universe status                       Show all 5 engine statuses + model config

universe mcp --stdio                  Start MCP server for Cursor

universe dashboard                    Open dashboard at localhost:3001

universe config set db <url>          Set PostgreSQL connection
universe config set premium_model <name>    Set premium model (regenerates workspace)
universe config set execution_model <name>  Set execution model (regenerates workspace + default)
universe config set developer_id <name>     Set developer identifier
universe config get db                Show database URL
universe config get models            Show model configuration
universe config reset                 Reset to defaults

universe db status                    Test database connection
universe db migrate                   Run migrations

universe skills list                  List active skills
universe skills lineage <name>        Show skill evolution
universe skills freeze <id>           Freeze a skill
universe skills unfreeze <id>         Unfreeze a skill

universe graph export                 Export graph JSON
universe graph export -o file.json    Export to file
```

---

---

# PART 2: DASHBOARD (dashboard.md + dashboard-build.md) — 4 Changes

---

## Change 1: Add Plans view to the dashboard

**ADD a new page: `dashboard/src/pages/Plans.jsx`**

This is a NEW tab in the sidebar between "Skills" and "Compression":

```
Sidebar navigation (updated):
  - Overview
  - Graph
  - Memory
  - Skills
  - Plans        ← NEW
  - Compression
  - Routing
```

**Plans page shows:**

1. **3 metric cards:**
   - Total plans (this month)
   - Completion rate (completed+verified / total)
   - Average steps per plan

2. **Plan list** — each row shows:
   - Title (truncated)
   - Status badge: pending (gray), executing (blue), completed (green), failed (red), verified (green+check), rejected (red+x)
   - Step count
   - Skill badge (if a skill was used and verified)
   - Planner model name
   - Executor model name
   - Timestamp

3. **Click a plan → expand to show:**
   - Full step list
   - Files to change vs files actually changed
   - Executor's result summary
   - Verification note (if verified/rejected)
   - Graph context and blast radius
   - Cost estimate for this plan

**API endpoint needed (add to dashboard handlers):**

```go
// GET /api/plans?developer=alice&status=verified&page=1&limit=20
func (s *Server) HandlePlansList(w http.ResponseWriter, r *http.Request)

// GET /api/plans/:id
func (s *Server) HandlePlanDetail(w http.ResponseWriter, r *http.Request)

// GET /api/plans/stats
func (s *Server) HandlePlanStats(w http.ResponseWriter, r *http.Request)
```

---

## Change 2: Update Routing view

**RENAME "Routing" to "Activity" in the sidebar.** It now shows the plan-based flow instead of internal LLM routing.

**REMOVE from routing page:**
- Internal model routing traces (Opus → Haiku → Opus internal calls)
- Escalation chain visualization
- Parallel execution diagram
- "Routing mode" breakdown (skill_execute, plan_execute, etc.)

**REPLACE with:**
- Plan lifecycle timeline for each task:
  ```
  → Plan created (Opus, 10:30 AM)
  → Executor picked up plan (10:31 AM)
  → Execution completed — 3 files changed (Haiku, 10:33 AM)
  → Planner verified — approved (Opus, 10:35 AM)
  ```
- Skill verification events ("Planner reviewed type-fix-v3 → approved for use")
- Cost per plan (estimated planner cost + executor cost vs all-premium)
- Plans that failed and were retried

**UPDATE the metric cards:**

```
BEFORE:                           AFTER:
Tasks today: 47                   Plans today: 12
Haiku ratio: 89%                  Verified: 10 (83%)
Cost today: $1.84                 Est. savings today: $8.40
Takeovers: 0                     Failed: 1 (retried)
```

---

## Change 3: Update Memory view

**REMOVE:**
- "by developer" filter dropdown (you only see your own)
- "Shared observations" metric card

**UPDATE descriptions:**
- Title: "Your Memory" instead of "Memory"
- Subtitle: "Your past session observations" instead of "Team observations"
- Empty state: "No observations yet. As you work with the planning and execution agents, your sessions will be saved here automatically."

---

## Change 4: Update Overview page

**ADD to the engine status strip:**

```
Engine 5 (Orchestrator):  ✅ Active
  12 plans today, 10 verified, premium: claude-opus-4, execution: claude-haiku-3.5
```

**ADD a plans summary section** below the cost chart:

```
Recent plans:
  ✓ Fix type mismatch in auth.ValidateToken — verified (2 hours ago)
  ✓ Add unit tests for gateway.LoginHandler — verified (5 hours ago)
  ✗ Refactor token.Serialize — rejected, retry pending (yesterday)
```

**UPDATE the cost chart:**
- X-axis: month
- Y-axis: estimated cost
- Two lines: "Actual (split model)" and "Would have been (all premium)"
- Data comes from `plan_costs` table, not the old `agent_costs` table

---

## Dashboard build changes (dashboard-build.md)

**ADD to Section 2.7 (page components):**

```
dashboard/src/pages/Plans.jsx — calls GET /api/plans, shows plan list with
status badges, step counts, model names. Click to expand full plan detail
with result and verification.
```

**ADD to Section 2.8 (shared components):**

```
dashboard/src/components/PlanTimeline.jsx — vertical timeline showing
plan → execute → verify lifecycle with timestamps and model badges.

dashboard/src/components/StatusBadge.jsx — colored badge for plan status
(pending/executing/completed/failed/verified/rejected).
```

**ADD to the API helper (dashboard/src/api.js):**

```javascript
// ADD to the api object:
plans:       (params) => fetchAPI('/plans', params),
planDetail:  (id)     => fetchAPI(`/plans/${id}`),
planStats:   ()       => fetchAPI('/plans/stats'),
```

**ADD to the Go backend routes (server.go registerRoutes):**

```go
s.mux.HandleFunc("/api/plans", s.HandlePlansList)
s.mux.HandleFunc("/api/plans/stats", s.HandlePlanStats)
s.mux.HandleFunc("/api/plans/", s.HandlePlanDetail)
```

**UPDATE the sidebar navigation in App.jsx:**

```jsx
// BEFORE:
const navItems = [
    { path: '/', label: 'Overview', icon: '📊' },
    { path: '/graph', label: 'Graph', icon: '🔗' },
    { path: '/memory', label: 'Memory', icon: '🧠' },
    { path: '/skills', label: 'Skills', icon: '🧬' },
    { path: '/compression', label: 'Compression', icon: '📐' },
    { path: '/routing', label: 'Routing', icon: '🔀' },
]

// AFTER:
const navItems = [
    { path: '/', label: 'Overview', icon: '📊' },
    { path: '/graph', label: 'Graph', icon: '🔗' },
    { path: '/memory', label: 'Your Memory', icon: '🧠' },
    { path: '/skills', label: 'Skills', icon: '🧬' },
    { path: '/plans', label: 'Plans', icon: '📋' },
    { path: '/compression', label: 'Compression', icon: '📐' },
    { path: '/activity', label: 'Activity', icon: '🔀' },
]
```

**ADD route in App.jsx:**

```jsx
// ADD:
import Plans from './pages/Plans'
import Activity from './pages/Activity'  // renamed from Routing

// In Routes:
<Route path="/plans" element={<Plans />} />
<Route path="/activity" element={<Activity />} />

// REMOVE:
// <Route path="/routing" element={<Routing />} />
```

---

---

# PART 3: NPM SETUP (npm-setup.md) — 4 Changes

---

## Change 1: Include workspace templates in the npm package

**ADD to the npm package `files` array in `npm/package.json`:**

```json
{
  "files": [
    "bin/",
    "scripts/",
    "templates/",
    "README.md",
    "LICENSE"
  ]
}
```

**Note:** The templates aren't actually shipped as files in the npm package — they're embedded in the Go binary via `go:embed`. The `universe setup` command generates them from embedded templates. So this change is actually NOT needed for the npm package itself. The Go binary is self-contained.

**HOWEVER**, for developers who want to customize the Cursor rules, provide a reference copy:

```
npm/
├── bin/
├── scripts/
├── templates/               ← NEW (reference copies only)
│   ├── planner.mdc.example
│   ├── executor.mdc.example
│   └── compression.mdc.example
├── README.md
└── LICENSE
```

These are just for reference — `universe setup` generates the actual files from embedded Go templates.

---

## Change 2: Update npm README quick start

**REPLACE the Quick Start section in `npm/README.md`:**

```markdown
## Quick start

```bash
# Install
npm install -g @atlas/universe

# Scan your project
cd my-project
universe init

# Configure models and generate Cursor workspace files
universe setup
# Pick your premium model (Opus, GPT-4o, Gemini Pro, etc.)
# Pick your execution model (Haiku, GPT-4o-mini, Flash, etc.)

# Open both Cursor windows
universe start
# 🧠 Planner window (premium) + ⚡ Executor window (cheap)

# Workflow:
#   In 🧠 Planner: "Fix the type mismatch in auth.ValidateToken"
#   In ⚡ Executor: "execute"
#   In 🧠 Planner: "verify"
```

## Connect database (optional — enables memory and skills)

```bash
docker compose up -d
universe config set db postgres://universe_admin:universe_secret_2024@localhost:5432/universe
universe db migrate
```
```

---

## Change 3: Update the command table in README

**REPLACE the commands table:**

```markdown
## Commands

| Command | What it does |
|---------|-------------|
| `universe init` | Scan codebase and build knowledge graph |
| `universe setup` | Interactive setup — pick models, generate config files |
| `universe plan` | Open planner Cursor window (premium model) |
| `universe exec` | Open executor Cursor window (execution model) |
| `universe start` | Open both windows |
| `universe status` | Show all 5 engine statuses + model config |
| `universe dashboard` | Open the dashboard (port 3001) |
| `universe config set db <url>` | Connect to PostgreSQL |
| `universe config set premium_model <name>` | Change premium model |
| `universe config set execution_model <name>` | Change execution model |
| `universe config get db` | Show database connection |
| `universe config get models` | Show model configuration |
| `universe db status` | Test database connection |
| `universe db migrate` | Run database migrations |
| `universe skills list` | List all active skills |
| `universe setup-rules` | Regenerate Cursor rules |
| `universe mcp --stdio` | Run MCP server (for Cursor connection) |
```

---

## Change 4: Update GitHub Actions workflow

**ADD workspace template files to the Go binary build step in `.github/workflows/release.yml`:**

No actual change needed — the workspace templates are embedded in the Go binary via `go:embed` in `setup.go`. The `go build` command already includes them. Just ensure the `internal/orchestrator/setup.go` file has the embedded template strings (which it does from the Engine 5 patch).

**UPDATE the README generation in the npm publish step** (if the workflow copies README to the npm folder):

```yaml
# In the npm-publish job, ensure the updated README is used:
- name: Publish to npm
  working-directory: ./npm
  run: npm publish --access public
  env:
    NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}
```

No other workflow changes needed — the Go binary handles everything.

---

---

# PART 4: CROSS-REFERENCE — What These Changes Affect

| Patch file | Affects | Changes in this patch |
|-----------|---------|----------------------|
| cli-wiring.md | `cmd/universe/setup.go` (new) | Full setup command with interactive model picker |
| cli-wiring.md | `cmd/universe/workspace.go` (new) | plan, exec, start commands |
| cli-wiring.md | `cmd/universe/config.go` | premium_model, execution_model, developer_id keys |
| cli-wiring.md | `cmd/universe/helpers.go` | UniverseConfig struct gets model fields |
| cli-wiring.md | `cmd/universe/status.go` | Footer shows model names |
| dashboard.md | Sidebar navigation | Add Plans, rename Routing to Activity |
| dashboard.md | Plans page | New page with plan list, timeline, status badges |
| dashboard.md | Activity page | Replaces Routing, shows plan lifecycle |
| dashboard.md | Memory page | Personal only, remove team features |
| dashboard.md | Overview page | Plans summary, model names in engine status |
| dashboard-build.md | App.jsx | New routes, updated nav items |
| dashboard-build.md | api.js | New plan endpoints |
| dashboard-build.md | server.go | New /api/plans routes |
| dashboard-build.md | Components | PlanTimeline, StatusBadge |
| npm-setup.md | README.md | Updated quick start, command table |
| npm-setup.md | package.json files | Reference template files |
