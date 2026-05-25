# Phase 01 — Universe ⇄ Cursor (stdio MCP) — Test Setup

**Goal of this phase:** Get Universe running as a local MCP server that Cursor can call, end-to-end, on a small test repo. No Anthropic API key. No hosted server. No team-shared state yet. Just one developer, one machine, one repo, one Cursor workspace.

**Definition of done:** A developer opens a Cursor task in the test repo, the agent automatically calls Universe MCP tools, Universe returns routing decisions / memory / skills from its database, and the agent uses those to complete the task. The orchestration cycle (premium plan → low-cost execute → premium verify) runs through Cursor's models.

**What we are NOT doing this phase:**
- No hosted/multi-tenant deployment
- No team-shared memory or skills (per-developer DB only)
- No model-tier USD cost tracking (Cursor owns the billing)
- No SSE/HTTP MCP transport (stdio only)
- No automatic outcome logging into memory yet (that's phase 2)

---

## 0. Decisions to lock before starting

| Decision | Choice for this phase | Why |
|---|---|---|
| MCP transport | **stdio** | Simplest. No network. Cursor spawns the binary. |
| Process scope | **Per-repo** (spawned with `--repo <path>`) | One Universe process per Cursor workspace. Avoids cross-repo state bleed. |
| Database | **PostgreSQL in Docker** | Already running locally. Skip SQLite swap. |
| Model slots | **User config in `~/.universe/config.yaml`** | `premium: <model_name>`, `low_cost: <model_name>`. Universe never calls models — these are advisory hints for the agent. |
| Routing output | **Role names, not model names** | `RoutingDecision.PlannerRole = "premium"` etc. The agent maps role → model. |
| Test repo | **TBD — pick a small Go or Python repo we control** | Needs to fit in graph in seconds, has known structure we can sanity-check. |

---

## 1. Today's work — ordered checklist

The order matters. Don't skip ahead — each step verifies the previous.

### Step 1 — Postgres ready (15 min)

- [ ] Confirm Docker Postgres container is running: `docker ps | grep postgres`
- [ ] Create the `universe` database: `docker exec -it <container> psql -U postgres -c "CREATE DATABASE universe;"`
- [ ] Apply migrations in order:
  - `psql -h localhost -U postgres -d universe -f migrations/002_memory_tables.sql`
  - `psql -h localhost -U postgres -d universe -f migrations/003_skills_tables.sql`
  - `psql -h localhost -U postgres -d universe -f migrations/004_cost_tracking.sql`
- [ ] Verify tables exist: `psql -d universe -c "\dt"` — expect memory_*, skills_*, agent_costs tables
- [ ] Write `.env` file at project root with `DATABASE_URL=postgres://postgres:<pwd>@localhost:5432/universe?sslmode=disable`

**Done when:** `psql -d universe -c "\dt"` lists tables from all three migrations.

### Step 2 — Pick the test repo and analyze it (15 min)

- [ ] Pick repo. Recommended: a small internal Go project, ~20–50 files. Avoid massive monorepos for the first test.
- [ ] Run: `universe analyze <test-repo-path>`
- [ ] Confirm `.universe/graph.json` exists in the test repo and has reasonable node/edge counts
- [ ] Sanity-check with: `universe query "what depends on <some_known_package>"` — should return real nodes

**Done when:** the graph file exists, `universe stats` shows non-zero packages/functions/imports, and a known query returns a sensible answer.

### Step 3 — Add user config for model slots (20 min)

New file: `internal/orchestrator/userconfig.go`

- [ ] Define `UserConfig` struct: `PremiumModel string`, `LowCostModel string`
- [ ] Loader reads `~/.universe/config.yaml` (or `$UNIVERSE_CONFIG` override)
- [ ] If file missing, write a default: `premium: claude-opus-4-7`, `low_cost: claude-haiku-4-5`
- [ ] **These names are advisory strings.** Universe does not validate them — the agent in Cursor maps them.

**Done when:** `universe config show` prints the current premium/low_cost mappings.

### Step 4 — Refactor `RoutingDecision` to role-based (30 min)

File: `internal/orchestrator/types.go`

- [ ] Replace `PlannerModel ModelTier` / `ExecutorModel ModelTier` with:
  - `PlannerRole string`  (values: `"premium"` or `"low_cost"`)
  - `ExecutorRole string`
  - `VerifierRole string`
- [ ] Add `NextStep string` to the decision (`"plan"`, `"execute"`, `"verify"`, `"direct"`, `"skill_execute"`, `"memory_apply"`)
- [ ] Update `router.go` to set role names instead of model tiers
- [ ] **Do not delete `llmclient.go` / `planner.go` / `executor.go` / `verifier.go`** — they're dormant on the Cursor path but still compile-tested. We'll remove or repurpose later.

**Done when:** `go build ./...` is clean and the router still passes its existing tests with updated assertions.

### Step 5 — Build the MCP server skeleton (1.5–2 hr)

New files:
```
internal/mcp/
├── server.go        # stdio JSON-RPC loop
├── tools.go         # tool registry + dispatch
├── tools_route.go   # tool: universe_route_task
├── tools_memory.go  # tool: universe_recall_memory
├── tools_skill.go   # tool: universe_get_skill
└── tools_test.go
cmd/universe/mcp_cmd.go   # `universe mcp` subcommand
```

- [ ] `cmd/universe mcp --repo <path>` subcommand registered with cobra
- [ ] Reads `DATABASE_URL`, loads graph from `<repo>/.universe/graph.json`, opens DB pool
- [ ] Speaks MCP over stdin/stdout — JSON-RPC 2.0
- [ ] Handles three MCP methods at minimum:
  - `initialize` → returns server info and capabilities
  - `tools/list` → returns the three tool definitions
  - `tools/call` → dispatches to the tool handler
- [ ] Three tools wired (only these for first test):
  - `universe_route_task` — input: `{task: string, graph_node_ids: string[]}` → output: `RoutingDecision` JSON
  - `universe_recall_memory` — input: `{graph_node_ids: string[]}` → output: relevant observations
  - `universe_get_skill` — input: `{graph_node_ids: string[], task_text: string}` → output: best skill match or `null`

**Don't worry about**: streaming, prompts, resources, sampling. Just tools.

**Done when:** the binary builds and starts without crashing on `universe mcp --repo <path>`.

### Step 6 — Smoke-test the MCP server WITHOUT Cursor (30 min)

This is the most important step. Debug failures here, not in Cursor.

- [ ] Install MCP Inspector: `npx @modelcontextprotocol/inspector universe mcp --repo <test-repo-path>`
- [ ] Inspector UI opens in browser. Confirm:
  - Server connects without error
  - Three tools listed in the UI
  - Click each tool, fill in inputs, click "Run"
  - Each returns valid JSON (not an error)
- [ ] If a tool errors: read the stderr the Inspector shows, fix, restart

**Done when:** All three tools return valid JSON for at least one test input via the Inspector.

### Step 7 — Connect to Cursor (15 min)

- [ ] Confirm Cursor version supports MCP (Settings → Features → look for "Model Context Protocol")
- [ ] Edit (or create) `~/.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "universe": {
      "command": "C:\\Users\\dedolk\\OneDrive - IFS\\Desktop\\Elavate\\Universe\\universe.exe",
      "args": ["mcp", "--repo", "<absolute-path-to-test-repo>"]
    }
  }
}
```

- [ ] Restart Cursor
- [ ] Open the test repo as a workspace
- [ ] In Cursor's MCP settings, confirm `universe` shows green/connected
- [ ] In a chat, ask the agent: *"What Universe tools do you have available?"* — confirm it lists the three tools

**Done when:** Cursor shows Universe as connected and the agent can name its tools.

### Step 8 — Add the cursor rule (10 min)

Create `<test-repo>/.cursorrules` (or `.cursor/rules` if newer Cursor):

```
For any code task on this repository:

1. First, call `universe_route_task` with the developer's request and any affected file/symbol IDs.
2. Read the returned `RoutingDecision`:
   - If `mode == "skill_execute"`: call `universe_get_skill`, follow its instruction, use the low-cost model.
   - If `mode == "memory_apply"`: use `universe_recall_memory`, apply the prior solution with the low-cost model.
   - If `mode == "plan_execute"` or `"full_orchestration"`:
     a. Use the PREMIUM model to write a structured plan matching the spec template.
     b. Use the LOW-COST model to execute each sub-task.
     c. Use the PREMIUM model to verify the output against the plan.
   - If `mode == "single_opus"` or `"single_haiku"`: use the indicated role for the whole task.
3. Before any decision, call `universe_recall_memory` to check for prior observations.
4. Always read the role names (`premium` / `low_cost`) from the routing decision and map to your configured models.

The PREMIUM model in this project is the one with higher capability (planning, verification).
The LOW-COST model is the one for bulk generation (execution).
```

**Done when:** A test prompt in Cursor triggers a `universe_route_task` call (visible in Cursor's tool-call panel).

### Step 9 — First end-to-end task (open-ended)

- [ ] Pick a small, well-scoped task on the test repo (e.g., "add a unit test for function X" or "rename variable Y in package Z").
- [ ] Ask Cursor to do it.
- [ ] Watch the tool-call panel: did the agent call `route_task`? Did it follow the returned mode? Did it switch models between plan and execute?
- [ ] Whatever happens, note it. **The first run will not be perfect.** That's fine. We're testing the wiring, not the results.

**Done when:** Cursor completes the task with at least one Universe tool call in the trace, and we have a clear list of what went wrong / right to iterate on tomorrow.

---

## 2. Files we will create today (summary)

```
internal/orchestrator/userconfig.go         # NEW — premium/low_cost mapping
internal/mcp/                               # NEW directory
  server.go                                 # NEW — stdio JSON-RPC loop
  tools.go                                  # NEW — tool registry
  tools_route.go                            # NEW
  tools_memory.go                           # NEW
  tools_skill.go                            # NEW
  tools_test.go                             # NEW
cmd/universe/mcp_cmd.go                     # NEW — `universe mcp` subcommand
~/.universe/config.yaml                     # NEW — user-side model slot mapping
~/.cursor/mcp.json                          # NEW (or edited) — Cursor MCP registration
<test-repo>/.cursorrules                    # NEW — agent rule for orchestration order
.env                                        # NEW — DATABASE_URL
```

## 3. Files we will modify today

```
internal/orchestrator/types.go              # RoutingDecision → role-based fields
internal/orchestrator/router.go             # set roles instead of model tiers
cmd/universe/main.go                        # register `mcp` subcommand
go.mod                                      # add MCP SDK dep if we use one (or none — handcraft JSON-RPC)
```

## 4. Files we are NOT touching today

```
internal/orchestrator/llmclient.go          # dormant on Cursor path
internal/orchestrator/planner.go            # dormant on Cursor path
internal/orchestrator/executor.go           # dormant on Cursor path
internal/orchestrator/verifier.go           # dormant on Cursor path
internal/orchestrator/escalation.go         # dormant on Cursor path
internal/orchestrator/parallel.go           # dormant on Cursor path
internal/orchestrator/tracker.go            # dormant — no USD tracking on Cursor path
```

Keep them. Don't delete. They become relevant if we ever add a hosted/CI path.

---

## 5. Open questions to resolve as we go

- **Which MCP Go SDK?** Options: `github.com/modelcontextprotocol/go-sdk` (official, may be early), `github.com/mark3labs/mcp-go` (community), or hand-rolled JSON-RPC (smallest dep). Start by checking the official SDK first; if it's not stable, hand-roll — the protocol is small.
- **How does the agent know which `graph_node_ids` to pass?** First version: the agent doesn't, and we pass `[]`. Universe's router degrades gracefully. Phase 2: add a `universe_resolve_symbols` tool that maps file paths / symbol names → graph node IDs.
- **Should `route_task` be called per task or per sub-step?** Per task for now. The agent gets one decision and follows it. Per-step routing can come later if we see misrouting.
- **What does `universe_recall_memory` return if memory is empty?** An empty array. Not an error. The agent should treat empty as "no prior knowledge, proceed normally."

---

## 6. Failure modes to expect (so we don't panic)

- **Cursor shows red/disconnected on the MCP entry.** Almost always a path issue in `mcp.json` or a startup crash. Run the binary manually first — it should accept stdin without dying.
- **Tools list shows up but every call returns "method not found".** JSON-RPC dispatch mismatch. The MCP spec is picky about method names — must be exactly `tools/call`, not `tool/call`.
- **The agent never calls Universe tools.** The `.cursorrules` isn't strong enough OR Cursor's agent decided not to use tools. Reword the rule to be more imperative. As a last resort, prefix the user prompt with "Use universe_route_task first" to confirm the tool path works.
- **Postgres connection fails inside the spawned MCP process.** Cursor spawns subprocesses with a clean env. Either pass `DATABASE_URL` via `env:` block in `mcp.json` or read `.env` from the repo directory.

---

## 7. What we'll know at the end of today

- Whether the MCP wiring is solid (binary, transport, tool dispatch)
- Whether Cursor's agent will actually use the tools when nudged by `.cursorrules`
- A list of concrete issues to fix in phase 02
- A baseline for what a Universe-assisted task looks like vs. a vanilla Cursor task

Tomorrow's phase 02 plan can then focus on: adding `get_plan_spec`, `verify_output`, `log_outcome`, and tightening the orchestration handoffs.
