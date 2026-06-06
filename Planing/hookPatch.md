# hookPatch.md — Universe Cursor-hook integration: findings + patch spec

**Audience:** the AI/maintainer working in the `@devhand/universe` **packaging repo**
(the npm package that ships `universe.exe`, runs `universe setup`, and generates
`.cursor/hooks.json` + `.cursor/rules/universe.mdc` on install).

**Purpose:** record exactly what is broken, what we verified, and the concrete
changes the packaging repo must make so that a fresh `npm install` + `universe setup`
produces a **working, token-cheap** Cursor integration — without relying on the
broken Cursor `postToolUse` injection path.

**Verified on:** Cursor `3.6.31` (Windows), `@devhand/universe` `v0.3.2`, June 2026.

---

## 1. TL;DR for the packaging repo

The current integration is built on a premise that **does not work on any current
Cursor build**: that a `PreToolUse`/`postToolUse` hook can inject a `[Universe]`
block into the model's context. It cannot. Two independent reasons:

1. **Cursor bug (platform):** `postToolUse` `additional_context` is accepted, logged,
   and validated by the hook runner but is **never surfaced to the model**. For
   non-MCP tools (Read/Grep/etc.) the hook response is discarded fire-and-forget.
   Confirmed by Cursor staff; tracked internally as ticket **T-C20310**. Still broken
   as of v3.7.2. The only injection path that works end-to-end is **`sessionStart`**.
2. **Our binary (shippable fix):** `universe hook-check` is a `PreToolUse` *permission*
   handler — it only ever prints `{"permission":"allow"}` and **cannot emit
   `additional_context`** at all. It also reads its input from **positional args**
   (`hook-check <tool_name> <tool_input_json>`), but native Cursor delivers the tool
   payload on **stdin**, so under Cursor it receives 0 args and errors out.

Additionally, the generated `.cursor/hooks.json` was in **Claude Code format**
(PascalCase keys, `tool_names` object matcher, nested `hook` object, `$TOOL_NAME`
env-var args), which **native Cursor silently ignores**.

**Net effect:** out of the box, the Universe hook never fires usefully, and even when
forced to fire it can never deliver context. Users get zero benefit and pay setup
complexity for it.

**The fix has two halves:**
- **Stop depending on context-injecting tool hooks.** Pivot to (a) `sessionStart`
  injection for a one-time digest and (b) on-demand CLI (`universe query/...`) driven
  by a lean always-apply rule. Both work today and are cheaper than MCP.
- **Emit native-Cursor-format config**, not Claude Code format.

---

## 2. What we changed in THIS repo (current on-disk state)

These are local experiments in the consuming repo, kept for reference. The packaging
repo should reproduce the *intent*, not necessarily these exact files.

| File | State | Notes |
|------|-------|-------|
| `.cursor/hooks.json` | `postToolUse` → `universe-context.ps1`, matcher `Grep`, timeout 10 | Native format. Fires correctly, emits valid `additional_context`, but model never receives it (the Cursor bug). |
| `.cursor/universe-context.ps1` | PowerShell bridge (diagnostic build) | Reads stdin JSON (handles a leading-byte prefix before `{`), extracts a symbol (skips Go keywords like `func`), runs `universe query`, emits `{"additional_context": ...}`. Also writes a debug log. **Proven working up to the EMIT step.** |
| `.cursor/universe-hook-debug.log` | diagnostic log | Hard evidence the hook fires (`CURSOR_VERSION=3.6.31`, full stdin) and emits, yet chat never shows the block. Safe to delete. |
| `.cursor/hook-probe.ps1` | minimal probe | Logged-only; used to prove both `preToolUse` and `postToolUse` fire. Safe to delete. |
| `.cursor/rules/universe.mdc` | `alwaysApply: true`, ~56 lines | **Inaccurate** — describes "PreToolUse hook prints the answer into your context", which never happens. Costs resident tokens every request for instructions that don't work. Must be rewritten. |

---

## 3. Verified technical facts (so the packaging repo doesn't re-derive them)

### 3.1 Native Cursor `.cursor/hooks.json` schema (NOT Claude Code)
```json
{
  "version": 1,
  "hooks": {
    "sessionStart": [
      { "command": "<shell string>", "timeout": 10 }
    ],
    "postToolUse": [
      { "command": "<shell string>", "matcher": "Read|Grep", "timeout": 10 }
    ]
  }
}
```
- `version`: number `1` (top level).
- Event keys are **camelCase**: `sessionStart`, `preToolUse`, `postToolUse`, etc.
- `matcher`: a **regex string** (e.g. `"Read|Grep"`), NOT an object with `tool_names`.
  Documented matcher tool types: `Shell`, `Read`, `Write`, `Grep`, `Delete`, `Task`,
  `MCP: <tool>`. Omit `matcher` to match all tools.
- `command`: a **plain shell string** (not an args array).
- `timeout`: number in **seconds** (not `timeout_ms`).
- `failClosed`: boolean, only meaningful on *gating* (before-*) hooks; omit elsewhere.
- There is **no** nested `"hook"` object and **no** `"type": "command"` field.

### 3.2 How Cursor passes data to a hook
- **NOT** via `$TOOL_NAME` / `$TOOL_INPUT` env vars (those do not exist).
- Tool payload arrives as **JSON on stdin**. On the tested build there are a few
  stray bytes before the opening `{` — parsers must slice from the first `{` to the
  last `}` (or otherwise tolerate a prefix) before `JSON.parse`.
- Available env vars: `CURSOR_PROJECT_DIR`, `CURSOR_VERSION`, `CURSOR_USER_EMAIL`,
  `CURSOR_TRANSCRIPT_PATH`, `CURSOR_CODE_REMOTE`, `CLAUDE_PROJECT_DIR`.

Observed `postToolUse` stdin (real, Cursor 3.6.31):
```json
{
  "conversation_id": "...", "generation_id": "...", "model": "claude-opus-4-8",
  "tool_name": "Grep",
  "tool_input": { "pattern": "func.*sendError", "output_mode": "content" },
  "tool_output": "{\"pattern\":\"func.*sendError\",\"success\":true}",
  "tool_use_id": "...", "duration": 103.3, "session_id": "...",
  "hook_event_name": "postToolUse", "cursor_version": "3.6.31",
  "workspace_roots": ["/c:/.../nexus-platform-service-automation"],
  "user_email": "...", "transcript_path": "..."
}
```
Note: `tool_output` for non-MCP tools is a tiny status blob
(`{"pattern":...,"success":true}`), **not** the actual search results.

### 3.3 Hook output schemas (per Cursor docs)
- `postToolUse` output: `{ "updated_mcp_tool_output": {...}, "additional_context": "..." }`
  → `additional_context` is **silently discarded for non-MCP tools** (the bug).
- `sessionStart` output: `{ "env": {...}, "additional_context": "..." }`
  → `additional_context` **is** injected into the conversation's initial system
  context (this path works).

### 3.4 `universe.exe` v0.3.2 behavior (verified)
- `universe hook-check <tool_name> <tool_input_json>` → prints `{"permission":"allow"}`.
  It is a `PreToolUse` permission gate; it does **not** and **cannot** emit
  `additional_context`. Reads positional args, not stdin.
- `universe query <symbol>` → produces the useful compact summary
  (definition, cluster, callers, callees, flows, impact). This is the real source of
  the "[Universe]" content.
- `universe init` / `analyze`, `search`, `deps`, `impact`, `stats`, `status` exist.

### 3.5 Graph DATA QUALITY problem (must-fix for trust)
On this repo the saved graph is unreliable for caller/impact:
- `universe stats` → **Files parsed: 21 / 76 (27.6%)**, Bytes parsed 2.6%.
- `universe query sendError` → **`Callers: none` / Impact: low — 0 callers**, but
  `rg` finds **~19 call sites** of `sendError` in the *same already-parsed* file.
- Re-running `universe init` did **not** change coverage or fix the caller count
  (completed in 166ms, identical 559 nodes / 3390 edges).

Conclusion: the analyzer reliably captures **definition location, type, cluster, and
callees (type/import references)** but **does not capture method call-edges**, so
**callers/impact are systematically undercounted**. Any feature that surfaces
caller/impact numbers will mislead the agent. Treat definition/location/callees as
trustworthy; treat callers/impact as unreliable until fixed.

---

## 4. Target design — "our own hook method" (works today, cheaper than MCP)

Goal: give the agent accurate, low-token code intelligence so it answers from the
graph instead of reading whole files — **without** MCP (resident tool-schema cost)
and **without** the broken `postToolUse` injection.

Three components, all generated by `universe setup` on `npm install`:

### A. `sessionStart` digest (the ONE working injection path) — PUSH, one-time
- A `sessionStart` hook runs `universe` to emit a **compact** repo digest and returns
  it as `{ "additional_context": "<digest>" }`.
- Digest content (keep it small — this is resident for the session):
  packages/clusters, key entry points, and a one-line "how to query on demand" note.
  **Do NOT dump caller/impact numbers** (unreliable, see §3.5).
- This is the only place `additional_context` actually reaches the model.

### B. On-demand CLI via Shell — PULL, pay-per-use (lowest cost)
- The agent runs `universe query|search|deps <symbol>` through the built-in Shell
  tool when it needs detail. No resident schema, no round-trip overhead, no hook
  dependency. Output enters context only when actually used.
- Ensure the binary is resolvable: either put `universe` on PATH during
  `npm install`, or have the rule reference the absolute install path
  (`%APPDATA%\npm\node_modules\@devhand\universe\bin\universe.exe` on Windows).
  Note: an earlier session found `universe` was NOT on PATH in PowerShell.

### C. Lean always-apply rule — replaces the inaccurate `universe.mdc`
Rewrite `.cursor/rules/universe.mdc` to:
- **Remove** all claims about a `PreToolUse`/`postToolUse` hook injecting a
  `[Universe]` block (false; wastes resident tokens).
- Instruct: "To locate a symbol or list its callees, run `universe query <symbol>`
  via the terminal instead of opening the file. Trust definition/location/callees;
  do **not** trust caller/impact counts (graph is partial)."
- Keep it short (a handful of lines) to minimize resident token cost.

### Why this beats the alternatives
- vs **MCP**: no resident tool schemas; reuses built-in Shell → far fewer tokens.
- vs **current hook**: doesn't depend on broken `postToolUse`; data path actually
  reaches the model (`sessionStart`) or is pulled on demand (CLI).

---

## 5. Action items for the packaging repo

1. **Fix `universe setup` / `setup-rules` output format.** Generate **native Cursor**
   `.cursor/hooks.json` (§3.1), not Claude Code format. Stop emitting PascalCase
   `PreToolUse`, `tool_names` object matchers, nested `hook` objects, and
   `$TOOL_NAME`/`$TOOL_INPUT` arg-passing.
2. **Stop wiring context injection to tool hooks.** Remove the `hook-check`-on-Grep/Read
   approach for context. Keep `hook-check` only if you genuinely want a permission gate
   (and make it read stdin JSON, not positional args).
3. **Add a `sessionStart` hook** that emits a compact digest via `additional_context`
   (§4.A). Provide a `universe` subcommand for it (e.g. `universe session-digest`)
   that reads stdin and prints the digest JSON.
4. **Make the binary stdin-aware** for any hook entrypoint: tolerate a byte prefix
   before `{`, parse `tool_name`/`tool_input`/`tool_output` from stdin, and never rely
   on env-var args.
5. **Rewrite the generated `universe.mdc`** to the lean on-demand model (§4.C); drop the
   "hook injects context" narrative.
6. **Resolve the binary path / PATH issue** so the agent can call `universe` from the
   terminal reliably (PATH entry on install, or document the absolute path in the rule).
7. **Fix graph coverage + caller edges (data quality).** Investigate why only 21/76
   files parse and why method call-edges aren't recorded (callers undercounted, §3.5).
   Until fixed, suppress caller/impact in any agent-facing output.
8. **Gate on Cursor's bug.** When Cursor ships the `postToolUse` `additional_context`
   fix (ticket T-C20310 / "next hooks iteration"), the dynamic per-tool-use injection
   becomes viable; until then, ship the `sessionStart` + on-demand design.

---

## 6. References
- Cursor Hooks docs: https://cursor.com/docs/hooks
- Bug (postToolUse additional_context not surfaced; staff-confirmed; ticket T-C20310):
  https://forum.cursor.com/t/native-posttooluse-hooks-accept-and-log-additional-context-successfully-but-the-injected-context-is-not-surfaced-to-the-model/155689
  - Still broken on v3.7.2 (post dated 2026-06-02); only `sessionStart` works end-to-end.
- sessionStart injection bug report (disputed reliability):
  https://forum.cursor.com/t/sessionstart-hook-additional-context-is-never-injected-into-agents-initial-system-context/158452
