package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Universe/universe/internal/compress"
)

func main() {
	level := flag.String("level", "compact", "compression level: compact | normal | full")
	task := flag.String("task", "Fix the type mismatch in auth.ValidateToken", "task prompt to compress")
	taskType := flag.String("task-type", "", "task type for full level: fix | test | pr | analysis")
	withGraph := flag.Bool("with-graph", true, "inject example graph context nodes")
	savings := flag.Bool("savings", false, "show estimated token savings and exit")
	flag.Parse()

	if *savings {
		fmt.Printf("LevelFull:    %.0f%% estimated token savings\n", compress.EstimateTokenSavings(compress.LevelFull)*100)
		fmt.Printf("LevelCompact: %.0f%% estimated token savings\n", compress.EstimateTokenSavings(compress.LevelCompact)*100)
		fmt.Printf("LevelNormal:  %.0f%% estimated token savings\n", compress.EstimateTokenSavings(compress.LevelNormal)*100)
		return
	}

	lvl := compress.CompressionLevel(*level)
	switch lvl {
	case compress.LevelFull, compress.LevelCompact, compress.LevelNormal:
	default:
		fmt.Fprintf(os.Stderr, "unknown level %q — use compact, normal, or full\n", *level)
		os.Exit(1)
	}

	cfg := compress.PromptConfig{Level: lvl}

	if *withGraph {
		cfg.GraphContext = exampleNodes()
	}

	if lvl == compress.LevelFull && *taskType != "" {
		cfg.TaskType = compress.TaskType(*taskType)
	}

	prompt := compress.BuildPrompt(*task, cfg)

	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  Level: %s   Task-type: %q   Graph nodes: %v\n", lvl, *taskType, *withGraph)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println(prompt)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  Estimated token savings: %.0f%%\n", compress.EstimateTokenSavings(lvl)*100)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
}

func exampleNodes() []compress.GraphNodeInfo {
	return []compress.GraphNodeInfo{
		{
			Name:        "ValidateToken",
			Kind:        "function",
			Package:     "auth",
			File:        "validate.go",
			Line:        42,
			Exported:    true,
			CallerNames: []string{"gateway.LoginHandler", "token.RefreshToken"},
			CalleeNames: []string{"crypto.VerifyJWT"},
		},
		{
			Name:        "LoginHandler",
			Kind:        "function",
			Package:     "gateway",
			File:        "handler.go",
			Line:        88,
			Exported:    true,
			CallerNames: []string{"api.Router"},
			CalleeNames: []string{"auth.ValidateToken", "middleware.RateLimit"},
		},
		{
			Name:        "TokenPayload",
			Kind:        "struct",
			Package:     "auth",
			File:        "models.go",
			Line:        15,
			Exported:    true,
			CalleeNames: []string{"auth.ValidateToken", "token.RefreshToken"},
		},
	}
}
