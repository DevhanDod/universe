package orchestrator

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
)

// WorkspaceGenerator creates .code-workspace files for the planner
// and executor Cursor windows, pre-configured with the developer's
// chosen models.

// GenerateWorkspaces creates both workspace files.
func GenerateWorkspaces(projectDir string, premiumModel string, executionModel string) error {
	wsDir := filepath.Join(projectDir, ".universe", "workspaces")
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		return err
	}

	planner := map[string]interface{}{
		"folders": []map[string]string{{"path": "../.."}},
		"settings": map[string]string{
			"ai.model":    premiumModel,
			"window.title": "🧠 Universe Planner — ${activeEditorShort}",
		},
	}

	executor := map[string]interface{}{
		"folders": []map[string]string{{"path": "../.."}},
		"settings": map[string]string{
			"ai.model":    executionModel,
			"window.title": "⚡ Universe Executor — ${activeEditorShort}",
		},
	}

	plannerJSON, _ := json.MarshalIndent(planner, "", "  ")
	if err := os.WriteFile(filepath.Join(wsDir, "planner.code-workspace"), plannerJSON, 0644); err != nil {
		return err
	}

	executorJSON, _ := json.MarshalIndent(executor, "", "  ")
	return os.WriteFile(filepath.Join(wsDir, "executor.code-workspace"), executorJSON, 0644)
}

// OpenPlannerWorkspace opens the planner workspace in Cursor.
// Called by: universe plan
func OpenPlannerWorkspace(projectDir string) error {
	wsPath := filepath.Join(projectDir, ".universe", "workspaces", "planner.code-workspace")
	return openInCursor(wsPath)
}

// OpenExecutorWorkspace opens the executor workspace in Cursor.
// Called by: universe exec, and store_plan MCP tool when AutoOpenExecutor is true.
func OpenExecutorWorkspace(projectDir string) error {
	wsPath := filepath.Join(projectDir, ".universe", "workspaces", "executor.code-workspace")
	return openInCursor(wsPath)
}

// openInCursor launches Cursor with the given workspace file.
// Falls back to VS Code if cursor command is not found.
func openInCursor(workspacePath string) error {
	if err := exec.Command("cursor", workspacePath).Start(); err == nil {
		return nil
	}
	return exec.Command("code", workspacePath).Start()
}
