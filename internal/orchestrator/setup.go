package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Setup generates all configuration files for the two-agent workflow.
// Called by: universe setup

// RunSetup generates all config files based on the developer's choices.
func RunSetup(projectDir string, premiumModel string, executionModel string, premiumCosts ModelConfig, executionCosts ModelConfig, dbURL string) error {
	if err := GenerateWorkspaces(projectDir, premiumModel, executionModel); err != nil {
		return err
	}
	if err := generateCursorRules(projectDir, premiumModel, executionModel); err != nil {
		return err
	}
	if err := generateMCPConfig(projectDir); err != nil {
		return err
	}
	return setDefaultModel(projectDir, executionModel)
}

// GenerateCursorRules is the exported wrapper for regenerating Cursor rule files.
func GenerateCursorRules(projectDir, premiumModel, executionModel string) error {
	return generateCursorRules(projectDir, premiumModel, executionModel)
}

// SetDefaultModel is the exported wrapper for updating the default model in .vscode/settings.json.
func SetDefaultModel(projectDir, executionModel string) error {
	return setDefaultModel(projectDir, executionModel)
}

// generateCursorRules creates the .cursor/rules/ directory and 3 rule files.
func generateCursorRules(projectDir string, premiumModel string, executionModel string) error {
	rulesDir := filepath.Join(projectDir, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return err
	}

	plannerRule := `---
description: "Universe planning agent — use with your PREMIUM model (` + premiumModel + `)"
globs: ["**/*"]
alwaysApply: false
---

You are the PLANNING agent. Your job is to analyze, check, and plan — NOT to write code.

For every task:
1. Call get_impact_analysis to understand the blast radius
2. Call find_skill to check for existing recipes
3. If a skill is found (requires_verification = true):
   - READ the skill instruction carefully
   - VERIFY it matches the current code (check file paths, function names)
   - If the skill has stale_warning = true, be EXTRA careful
   - If verified: include skill steps in your plan
   - If outdated: ignore the skill, plan from scratch
4. Call recall_memory to check your past work on this code
5. Write a step-by-step plan (be specific: file paths, line numbers, exact changes)
6. Call store_plan to save the plan

Do NOT write code. Do NOT edit files. Only analyze and plan.
After storing the plan, tell the developer: "Plan stored. Switch to the executor window."
`
	if err := os.WriteFile(filepath.Join(rulesDir, "universe-planner.mdc"), []byte(plannerRule), 0644); err != nil {
		return err
	}

	executorRule := `---
description: "Universe execution agent — use with your EXECUTION model (` + executionModel + `)"
globs: ["**/*"]
alwaysApply: false
---

You are the EXECUTION agent. Your job is to follow plans and write code — NOT to analyze or re-think.

When the developer says "execute", "run plan", or "do the task":
1. Call get_plan to retrieve the latest pending plan
2. Read each step carefully
3. Follow each step EXACTLY as written — do not deviate
4. Write the code changes
5. Run the tests if specified in the plan
6. Call store_plan_result with:
   - success: true/false
   - summary: what you did
   - files changed
   - tests passed or failed
   - error detail if anything went wrong

Do NOT re-analyze the problem. Do NOT question the plan. Do NOT skip steps.
After storing the result, tell the developer: "Done. Switch to the planner window to verify."
`
	if err := os.WriteFile(filepath.Join(rulesDir, "universe-executor.mdc"), []byte(executorRule), 0644); err != nil {
		return err
	}

	compressionRule := `---
description: "Universe output compression — reduces token waste"
globs: ["**/*"]
alwaysApply: true
---

Output rules (apply to EVERY response):
- No "I'd be happy to help", "Sure!", "Great question!", "Let me help you with that"
- No "It might be worth considering", "Perhaps you could", "You may want to"
- No "Let me explain", "As you can see", "It's important to note that"
- Keep ALL code blocks, function names, variable names, error messages EXACT
- Max 2 sentences for explanations unless the developer asks for more
- If code alone answers the question, output code only — no surrounding prose
- For git commits and PR descriptions, write normally (not compressed)
`
	return os.WriteFile(filepath.Join(rulesDir, "universe-compression.mdc"), []byte(compressionRule), 0644)
}

// generateMCPConfig creates .cursor/mcp.json if it doesn't already exist.
func generateMCPConfig(projectDir string) error {
	mcpPath := filepath.Join(projectDir, ".cursor", "mcp.json")
	if _, err := os.Stat(mcpPath); err == nil {
		return nil // already exists — don't overwrite
	}

	mcpConfig := map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"universe": map[string]interface{}{
				"command": "universe",
				"args":    []string{"mcp", "--repo", "."},
			},
		},
	}

	if err := os.MkdirAll(filepath.Join(projectDir, ".cursor"), 0755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(mcpConfig, "", "  ")
	return os.WriteFile(mcpPath, data, 0644)
}

// setDefaultModel updates .vscode/settings.json with the execution model as default.
func setDefaultModel(projectDir string, executionModel string) error {
	settingsPath := filepath.Join(projectDir, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Join(projectDir, ".vscode"), 0755); err != nil {
		return err
	}

	settings := map[string]interface{}{}
	if data, err := os.ReadFile(settingsPath); err == nil {
		json.Unmarshal(data, &settings) //nolint:errcheck
	}

	settings["ai.model"] = executionModel

	data, _ := json.MarshalIndent(settings, "", "  ")
	return os.WriteFile(settingsPath, data, 0644)
}
