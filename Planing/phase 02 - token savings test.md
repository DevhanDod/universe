# Phase 02 — Token Savings Verification Test

**Goal:** Prove (or disprove) that Universe + Cursor uses fewer tokens than Cursor alone for the same set of tasks. If we can't measure it, we can't claim it.

**End goal of this test:** A spreadsheet with two columns — "Cursor alone" tokens and "Cursor + Universe" tokens — across 10+ identical tasks. If Universe saves tokens, the second column is consistently smaller. If it doesn't, we learn what to fix.

---

## 1. First — clear up "how it runs behind"

With **stdio MCP** (the phase 01 transport), you do NOT run Universe in the background yourself. Cursor spawns it.

What actually happens:
1. You add the `universe` entry to `~/.cursor/mcp.json`
2. When Cursor starts, it reads that config and **launches `universe.exe mcp --repo <path>` as a child process**
3. Cursor talks to that child over stdin/stdout
4. When you close Cursor, the child dies
5. Next time you open Cursor, it spawns a fresh one

So there is **no Windows service, no Task Scheduler, no terminal to keep open**. Cursor is the launcher.

You'd only need to run Universe as a background service if you go to the hosted (HTTP) transport in a later phase. Not today.

**Where the logs go:** Universe's stderr is captured by Cursor and shown in Cursor's MCP debug panel (Settings → MCP → click your server name). For deeper debugging, redirect stderr to a file by writing a wrapper script. We'll set this up below.

---

## 2. What does "saving tokens" actually mean here?

Cursor owns the LLM bill, not us. So "saving tokens" is **not** a USD number we can pull from an Anthropic dashboard. It's something we have to define and measure ourselves.

There are three distinct savings mechanisms in Universe, and we need to measure each:

### Mechanism A: Prompt compression (Engine 4)
**What it saves:** The agent would normally read large chunks of source code into context. Universe's compression returns a shorthand representation that's 50–80% smaller for the same information density.

**How to measure:** Universe logs, for every `compress_context` call: input character count, output character count, estimated token delta.

### Mechanism B: Skill recipe injection (Engine 3)
**What it saves:** Instead of the agent re-deriving how to do a known task type (e.g., "add a unit test in this codebase's style"), Universe hands it a recipe. The agent skips re-exploration.

**How to measure:** Count tool calls and round-trips with and without skills. Fewer round-trips = fewer tokens.

### Mechanism C: Memory recall (Engine 2)
**What it saves:** When the same kind of bug or refactor has been done before, the agent gets the prior solution as context instead of re-discovering it.

**How to measure:** Same as skills — count of round-trips, plus a qualitative "did it find the answer faster?"

### What we'll report at the end of this test
For each of 10 tasks, four numbers:
1. **Total tokens used** (from Cursor's usage UI)
2. **Tool round-trips** (Cursor shows this in the chat trace)
3. **Wall-clock time** (start-to-finish stopwatch)
4. **Subjective quality** (did the output work?)

Universe's claim is validated if column 1 drops materially (target: 30%+ reduction) without column 4 getting worse.

---

## 3. The test environment

### 3.1 Machine setup checklist

- [ ] Cursor installed, latest version
- [ ] Cursor's **Usage** panel accessible (Settings → Billing → Usage, or similar — confirm it shows per-request token counts)
- [ ] Postgres running in Docker
- [ ] Universe phase 01 build complete (MCP server runs, Inspector smoke-test passes)
- [ ] A test repo of ~20–50 files, **freshly cloned** (no Cursor history, no `.cursor` folder yet)
- [ ] Universe binary at a stable path (no spaces in the path is safer; if path has spaces like "OneDrive - IFS", quote it carefully in `mcp.json`)
- [ ] **A blank spreadsheet** (Excel, Google Sheets, or even a markdown table) with these columns:
  - Task #, Task description, Mode (baseline/universe), Total tokens, Tool calls, Wall time (sec), Output worked? (Y/N), Notes

### 3.2 The two Cursor configurations we'll switch between

You need to be able to flip Universe on and off cleanly. The easiest way:

**Config A — Baseline (Universe OFF):**
- `mcp.json` has no `universe` entry (or it's commented out / renamed to `universe-disabled`)
- Repo has NO `.cursorrules` file
- Cursor restarted

**Config B — With Universe (Universe ON):**
- `mcp.json` includes the `universe` entry
- Repo has `.cursorrules` with the orchestration rule (from phase 01 step 8)
- Cursor restarted, MCP shows green/connected

Restarting Cursor between flips is non-negotiable — MCP state doesn't always reload cleanly.

### 3.3 Universe-side instrumentation (build this BEFORE testing)

Add a single log file Universe writes to so we can audit what it actually did:

**New file: `internal/mcp/audit.go`**

- Append-only log at `~/.universe/audit.log`
- One JSON line per tool call:
  ```json
  {"ts":"2026-05-24T14:22:01Z","tool":"compress_context","input_chars":4820,"output_chars":1340,"input_tokens_est":1205,"output_tokens_est":335,"saved_tokens_est":870,"repo":"<repo-id>","task_hint":"<first 80 chars of task>"}
  ```
- Use a rough token estimator: `chars / 4` is good enough for English/code mix. Don't bother with tiktoken yet.
- Logging must be non-blocking (goroutine + buffered channel, drop if backlog) so it never slows the tool call

This gives us an objective record: "Universe was asked to compress X bytes of context, it returned Y bytes, estimated tokens saved = Z." Without this we'll be guessing.

---

## 4. The test protocol

### 4.1 Design the task set (do this first, before any measurement)

Pick **10 tasks** representative of real work. They must be:
- **Deterministic enough to repeat** — "fix this specific bug" not "improve this code"
- **Small enough to complete in <5 min** each
- **Varied** — mix of: code fix, test generation, refactor, PR description, documentation, dependency check, analysis question, config tweak, error explanation, cross-file rename

Example task list (you'll write the actual ones based on the test repo):

| # | Task description | Expected mode |
|---|---|---|
| 1 | Add a unit test for function `ValidateToken` in `auth/validate.go` | test_gen |
| 2 | Fix the off-by-one error in `pkg/parser/scan.go:142` | code_fix |
| 3 | Rename variable `usr` to `user` across the `internal/auth` package | refactor |
| 4 | Write a PR description for the last commit | pr_gen |
| 5 | Explain what `Compressor.BuildPrompt` does and where it's called | explanation |
| 6 | Add error handling to `db.Connect` for connection refused | code_fix |
| 7 | What would break if I remove the `cobra` dependency? | analysis |
| 8 | Generate a config schema file for the new `routing.yaml` | config_change |
| 9 | Refactor `parseLine` to use a switch statement instead of if-else | refactor |
| 10 | Add a doc comment to every exported function in `internal/graph/` | code_fix |

Write these into a `tasks.md` file in the test repo so you copy-paste the exact same prompt twice — once in baseline, once with Universe. Wording must be identical between runs.

### 4.2 Test run protocol — Baseline pass (Config A)

For each task, in order:

1. Open Cursor on the test repo (Config A active — no Universe, no `.cursorrules`)
2. Start a fresh chat (clean context — no carryover from previous task)
3. Open the Cursor usage panel in a separate window so you can see token counts update
4. Note the "tokens used so far" number BEFORE the task
5. Start your stopwatch
6. Paste the task description from `tasks.md`
7. Let the agent complete it. If it asks clarifying questions, answer minimally and consistently.
8. Stop the stopwatch
9. Note the "tokens used" number AFTER the task — subtract to get task tokens
10. Note the number of tool calls in the chat trace (Cursor shows them)
11. Test the output — does the code compile / test pass / PR text make sense?
12. Record the row in your spreadsheet
13. Close the chat, **don't** carry state to the next task

Do all 10 tasks. **Don't accept the changes** to the repo — discard them between tasks, or the next task starts with a polluted state. Use git: `git stash && git reset --hard` between tasks.

### 4.3 Test run protocol — Universe pass (Config B)

1. Switch to Config B (enable Universe in `mcp.json`, add `.cursorrules`, restart Cursor)
2. Confirm MCP green in Cursor settings
3. Run `universe analyze <test-repo>` once to populate the graph
4. (Optional but recommended) Pre-seed memory with 1–2 plausible observations so memory recall has something to find. Even one entry tests the recall path.
5. Repeat the 10 tasks, **using exactly the same prompts**, recording the same four numbers per task
6. Watch the tool-call panel — note which Universe tools the agent called per task
7. After all 10, dump the `~/.universe/audit.log` and append a summary column to the spreadsheet: "Universe-reported saved_tokens (estimated)"

### 4.4 Sanity checks while running

If during the Universe pass you notice:
- The agent **never calls Universe tools** → `.cursorrules` isn't strong enough. Stop, fix the rule, restart. Don't continue measuring — the data is meaningless.
- The agent calls Universe tools but the **output is worse** → critical finding. Stop, investigate which tool returned bad data.
- The agent calls Universe tools and **token count goes up** → possible: Universe is adding context, not replacing it. Investigate compression output size.

---

## 5. Analyzing the results

After both passes, your spreadsheet has 20 rows. Compute three things:

### 5.1 Per-task delta
For each task: `tokens_baseline - tokens_universe = saved`. Express as percentage of baseline.

### 5.2 Aggregate
- **Total tokens saved across 10 tasks**
- **Average % savings** (weighted by baseline token count, not flat average)
- **Median % savings** (less swayed by one big outlier)
- **Worst case** — was there a task where Universe HURT?

### 5.3 Where the savings came from
Group tasks by which Universe tools fired:
- Tasks with skill match (skill recipe used) — savings %
- Tasks with memory hit — savings %
- Tasks with compression only — savings %
- Tasks where Universe matched nothing (fell through to single_opus mode) — savings should be ~0 here, validates that we're not making things worse

### 5.4 Decision points
The result tells you what to do next:

| Result | Meaning | Next action |
|---|---|---|
| Average savings > 30%, quality unchanged | **Working as designed** | Move to phase 03 (add remaining tools, second dev) |
| Savings 10–30%, quality unchanged | **Real but small** | Investigate which engines are underperforming. Probably compression underused or skills empty. |
| Savings < 10%, quality unchanged | **Noise, not real** | The agent isn't using Universe enough. Strengthen `.cursorrules`, improve tool descriptions. |
| Savings positive but **quality worse** | **Broken** | The agent is following Universe into wrong solutions. Look at routing accuracy first. |
| Tokens UP with Universe | **Universe is adding overhead** | Critical bug. Likely the agent is reading Universe outputs without replacing the corresponding native exploration. |

---

## 6. Pitfalls and how to avoid them

### "I can't see token counts in Cursor"
Cursor's exposure of token usage has changed across versions. If your version doesn't show per-request tokens, alternatives:
- Cursor's billing dashboard usually has daily/hourly aggregates — do the baseline pass and Universe pass on **different days** and read aggregates
- Count input/output **characters** in the chat as a proxy (`chars / 4` ≈ tokens)
- Watch the Cursor Pro usage meter "fast requests remaining" delta — each fast request maps roughly to a fixed quota

Whatever you use, **use the SAME method for both passes**. Apples to apples.

### "Cursor's caching skews the results"
Cursor caches model responses for identical contexts. If you run baseline then immediately run Universe with similar context, the second pass may benefit from cache.
**Mitigation:** Run Universe pass first, baseline second. Or insert a small no-op change in the repo between passes to bust the cache. Or do passes on different days.

### "The agent does the task differently each run, even with the same prompt"
True. LLMs are non-deterministic.
**Mitigation:** Run each task 3 times per config and average. 10 tasks × 2 configs × 3 runs = 60 trials. Painful but the only way to separate signal from noise.
**If you only have time for one run per task:** trust the aggregate across 10 tasks more than any individual delta.

### "The test repo is too small / too simple to show savings"
Possible. Universe shines on larger codebases where graph context and skill recipes pay off most.
**Mitigation:** If results are weak, repeat on a second, larger test repo before concluding Universe doesn't work.

### "Universe's audit log shows big savings but Cursor's token count doesn't drop"
Means the agent is reading Universe's output AND still doing its own exploration. The `.cursorrules` needs to be tighter — tell the agent to *trust* Universe and not re-derive.

---

## 7. Today's actionable steps (in order)

If you want to run this test, the work breaks into 4 sittings:

### Sitting 1 — Instrumentation (1–2 hr)
- [ ] Build `internal/mcp/audit.go` (the JSON-line logger)
- [ ] Wire audit logging into the three tools (`route_task`, `recall_memory`, `get_skill`) — and `compress_context` if it's already a tool
- [ ] Verify the log file appears at `~/.universe/audit.log` after one MCP Inspector call

### Sitting 2 — Task set (30 min)
- [ ] Pick the test repo
- [ ] Write 10 tasks into `tasks.md` in the repo
- [ ] Confirm each task is deterministic and small enough

### Sitting 3 — Baseline pass (1–1.5 hr)
- [ ] Config A active
- [ ] Run all 10 tasks, record spreadsheet rows 1–10
- [ ] Git reset between tasks

### Sitting 4 — Universe pass (1.5–2 hr)
- [ ] Config B active, MCP green
- [ ] Run all 10 tasks with identical prompts, record rows 11–20
- [ ] Append audit log summary

### Then: analysis (30 min)
- [ ] Compute the three aggregates
- [ ] Decide which decision-point row you hit
- [ ] Write a short note: "Phase 02 result: X% average savings, Y quality, next step is Z."

---

## 8. What we'll do with the result

If the savings are real:
- Tighten the test (more tasks, more reps) to confirm
- Move to phase 03 (hosted, second dev)
- Start the manager-facing dashboard

If the savings are not real:
- We've spent ~1 day finding out instead of ~1 quarter building hosted infra on a wrong premise
- Fix the underperforming engine(s)
- Re-test before any further investment

This phase is cheap relative to what it tells us. Don't skip it.
