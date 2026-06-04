package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/memory"
	"github.com/Universe/universe/internal/orchestrator"
	"github.com/Universe/universe/internal/skills"
)

// v0.2.8 shell write commands. Reads went back to MCP because the
// agent ignored shell-only reads; writes moved here for the opposite
// reason — they don't need to be in the per-turn MCP schema list
// (the planner / executor only writes occasionally) and structured
// input fits flags + stdin fine.

// ──────────────────────────────────────────────────────────────────────
// universe store-observation
// ──────────────────────────────────────────────────────────────────────

var (
	storeObsCategory string
	storeObsSummary  string
	storeObsNode     string
	storeObsDetail   string
	storeObsShared   bool
)

var storeObservationCmd = &cobra.Command{
	Use:   "store-observation",
	Short: "Save a fix, pattern, or decision to personal memory",
	RunE:  runStoreObservation,
}

func runStoreObservation(_ *cobra.Command, _ []string) error {
	if storeObsCategory == "" || storeObsSummary == "" {
		return fmt.Errorf("--category and --summary are required")
	}
	detail := storeObsDetail
	if detail == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
		detail = strings.TrimSpace(string(b))
	}
	dbURL := GetDBURL()
	if dbURL == "" {
		return fmt.Errorf("no database configured")
	}
	store, err := memory.NewStore(dbURL)
	if err != nil {
		return fmt.Errorf("connect memory: %w", err)
	}
	defer store.Close()

	obs := memory.Observation{
		DeveloperID: getDeveloperID(),
		GraphNodeID: storeObsNode,
		Category:    storeObsCategory,
		Summary:     storeObsSummary,
		Detail:      detail,
		Confidence:  1.0,
		Shared:      storeObsShared,
	}
	saved, err := store.InsertObservation(obs)
	if err != nil {
		return fmt.Errorf("insert observation: %w", err)
	}
	fmt.Printf("Stored observation %s", saved.ID)
	if saved.GraphNodeID != "" {
		fmt.Printf(" tagged to %s", saved.GraphNodeID)
	}
	fmt.Println()
	return nil
}

// ──────────────────────────────────────────────────────────────────────
// universe report-skill
// ──────────────────────────────────────────────────────────────────────

var (
	reportSkillID      string
	reportSkillSuccess bool
	reportSkillFailure bool
	reportSkillOutput  string
	reportSkillError   string
)

var reportSkillCmd = &cobra.Command{
	Use:   "report-skill",
	Short: "Report skill execution success or failure",
	RunE:  runReportSkill,
}

func runReportSkill(_ *cobra.Command, _ []string) error {
	if reportSkillID == "" {
		return fmt.Errorf("--skill-id is required")
	}
	if reportSkillSuccess == reportSkillFailure {
		return fmt.Errorf("provide exactly one of --success or --failure")
	}
	dbURL := GetDBURL()
	if dbURL == "" {
		return fmt.Errorf("no database configured")
	}
	store, err := skills.NewStore(dbURL)
	if err != nil {
		return fmt.Errorf("connect skills: %w", err)
	}
	defer store.Close()

	exec := skills.NewExecutor(store, skills.DefaultConfig())
	execution := skills.SkillExecution{
		SkillID:     reportSkillID,
		DeveloperID: getDeveloperID(),
		Success:     reportSkillSuccess,
		TaskOutput:  reportSkillOutput,
		ErrorDetail: reportSkillError,
	}
	if err := exec.RecordExecution(reportSkillID, execution); err != nil {
		return fmt.Errorf("record execution: %w", err)
	}
	fmt.Printf("Recorded execution for skill %s (success=%v)\n", reportSkillID, reportSkillSuccess)
	return nil
}

// ──────────────────────────────────────────────────────────────────────
// universe store-plan / store-plan-result / verify-plan
// ──────────────────────────────────────────────────────────────────────

var (
	storePlanTitle  string
	storePlanTask   string
	storePlanSteps  []string
	storePlanRisk   string
	storePlanFiles  []string
)

var storePlanCmd = &cobra.Command{
	Use:   "store-plan",
	Short: "Save a step-by-step plan for the executor agent",
	RunE:  runStorePlan,
}

func runStorePlan(_ *cobra.Command, _ []string) error {
	if storePlanTitle == "" || len(storePlanSteps) == 0 {
		return fmt.Errorf("--title and at least one --step are required")
	}
	dbURL := GetDBURL()
	if dbURL == "" {
		return fmt.Errorf("no database configured")
	}
	ps, err := orchestrator.NewPlanStore(dbURL)
	if err != nil {
		return fmt.Errorf("connect plan store: %w", err)
	}
	defer ps.Close()

	plan, err := ps.StorePlan(orchestrator.Plan{
		DeveloperID:   getDeveloperID(),
		Title:         storePlanTitle,
		TaskPrompt:    storePlanTask,
		Steps:         storePlanSteps,
		FilesToChange: storePlanFiles,
		RiskLevel:     storePlanRisk,
		Status:        orchestrator.PlanPending,
	})
	if err != nil {
		return fmt.Errorf("store plan: %w", err)
	}
	fmt.Printf("Stored plan %s\n", plan.ID)
	return nil
}

var (
	storePlanResultID      string
	storePlanResultSuccess bool
	storePlanResultSummary string
	storePlanResultFiles   []string
	storePlanResultTests   bool
	storePlanResultError   string
)

var storePlanResultCmd = &cobra.Command{
	Use:   "store-plan-result",
	Short: "Report execution result for a plan",
	RunE:  runStorePlanResult,
}

func runStorePlanResult(_ *cobra.Command, _ []string) error {
	if storePlanResultID == "" {
		return fmt.Errorf("--plan-id is required")
	}
	dbURL := GetDBURL()
	if dbURL == "" {
		return fmt.Errorf("no database configured")
	}
	ps, err := orchestrator.NewPlanStore(dbURL)
	if err != nil {
		return fmt.Errorf("connect plan store: %w", err)
	}
	defer ps.Close()
	if err := ps.StorePlanResult(storePlanResultID, storePlanResultSuccess,
		storePlanResultSummary, storePlanResultFiles,
		storePlanResultTests, storePlanResultError); err != nil {
		return fmt.Errorf("store result: %w", err)
	}
	fmt.Printf("Result recorded for plan %s (success=%v)\n", storePlanResultID, storePlanResultSuccess)
	return nil
}

var (
	verifyPlanID      string
	verifyPlanApprove bool
	verifyPlanReject  bool
	verifyPlanNote    string
)

var verifyPlanCmd = &cobra.Command{
	Use:   "verify-plan",
	Short: "Approve or reject an executor's plan result",
	RunE:  runVerifyPlan,
}

func runVerifyPlan(_ *cobra.Command, _ []string) error {
	if verifyPlanID == "" {
		return fmt.Errorf("--plan-id is required")
	}
	if verifyPlanApprove == verifyPlanReject {
		return fmt.Errorf("provide exactly one of --approve or --reject")
	}
	dbURL := GetDBURL()
	if dbURL == "" {
		return fmt.Errorf("no database configured")
	}
	ps, err := orchestrator.NewPlanStore(dbURL)
	if err != nil {
		return fmt.Errorf("connect plan store: %w", err)
	}
	defer ps.Close()
	if err := ps.VerifyPlan(verifyPlanID, verifyPlanApprove, verifyPlanNote); err != nil {
		return fmt.Errorf("verify plan: %w", err)
	}
	verdict := "approved"
	if verifyPlanReject {
		verdict = "rejected"
	}
	fmt.Printf("Plan %s %s\n", verifyPlanID, verdict)
	return nil
}

func init() {
	storeObservationCmd.Flags().StringVar(&storeObsCategory, "category", "", "fix | pattern | decision | discovery")
	storeObservationCmd.Flags().StringVar(&storeObsSummary, "summary", "", "one-sentence summary (required)")
	storeObservationCmd.Flags().StringVar(&storeObsNode, "node", "", "graph node ID to tag")
	storeObservationCmd.Flags().StringVar(&storeObsDetail, "detail", "", "full detail (use '-' for stdin)")
	storeObservationCmd.Flags().BoolVar(&storeObsShared, "shared", false, "share with team")
	rootCmd.AddCommand(storeObservationCmd)

	reportSkillCmd.Flags().StringVar(&reportSkillID, "skill-id", "", "skill ID (required)")
	reportSkillCmd.Flags().BoolVar(&reportSkillSuccess, "success", false, "skill worked")
	reportSkillCmd.Flags().BoolVar(&reportSkillFailure, "failure", false, "skill failed")
	reportSkillCmd.Flags().StringVar(&reportSkillOutput, "output", "", "what happened")
	reportSkillCmd.Flags().StringVar(&reportSkillError, "error", "", "what went wrong")
	rootCmd.AddCommand(reportSkillCmd)

	storePlanCmd.Flags().StringVar(&storePlanTitle, "title", "", "plan title (required)")
	storePlanCmd.Flags().StringVar(&storePlanTask, "task", "", "task prompt")
	storePlanCmd.Flags().StringSliceVar(&storePlanSteps, "step", nil, "plan step (repeatable, required)")
	storePlanCmd.Flags().StringVar(&storePlanRisk, "risk", "", "low | medium | high")
	storePlanCmd.Flags().StringSliceVar(&storePlanFiles, "file", nil, "file to change (repeatable)")
	rootCmd.AddCommand(storePlanCmd)

	storePlanResultCmd.Flags().StringVar(&storePlanResultID, "plan-id", "", "plan ID (required)")
	storePlanResultCmd.Flags().BoolVar(&storePlanResultSuccess, "success", false, "execution succeeded")
	storePlanResultCmd.Flags().StringVar(&storePlanResultSummary, "summary", "", "what was done")
	storePlanResultCmd.Flags().StringSliceVar(&storePlanResultFiles, "file", nil, "file changed (repeatable)")
	storePlanResultCmd.Flags().BoolVar(&storePlanResultTests, "tests-passed", false, "tests passed")
	storePlanResultCmd.Flags().StringVar(&storePlanResultError, "error", "", "error detail")
	rootCmd.AddCommand(storePlanResultCmd)

	verifyPlanCmd.Flags().StringVar(&verifyPlanID, "plan-id", "", "plan ID (required)")
	verifyPlanCmd.Flags().BoolVar(&verifyPlanApprove, "approve", false, "approve the result")
	verifyPlanCmd.Flags().BoolVar(&verifyPlanReject, "reject", false, "reject the result")
	verifyPlanCmd.Flags().StringVar(&verifyPlanNote, "note", "", "verification note")
	rootCmd.AddCommand(verifyPlanCmd)
}
