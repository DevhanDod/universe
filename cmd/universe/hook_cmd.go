package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/hookengine"
)

// universe hook-check — invoked by .cursor/hooks.json before the agent
// runs Read/Grep/Search-style tools. v0.3.0 makes the hook deliver the
// complete graph answer (callers, callees, flows, impact, confidence)
// directly into the agent's context, rather than a one-line "consider
// using X instead" nudge. The goal is to make the file read unnecessary
// when the graph already covers the question, the same model Graphify
// uses.
//
// Silent cases (intentional — the tool call should proceed normally):
//   - Graph not built yet (no .universe/graph.json)
//   - Tool input doesn't yield a queryable symbol
//   - Graph has no node matching the symbol
//   - Confidence is too low to trust the graph answer

var hookCheckCmd = &cobra.Command{
	Use:    "hook-check <tool_name> <tool_input_json>",
	Short:  "PreToolUse hook handler (called by Cursor, not by hand)",
	Hidden: true,
	Args:   cobra.ExactArgs(2),
	RunE:   runHookCheck,
}

func init() {
	rootCmd.AddCommand(hookCheckCmd)
}

func runHookCheck(_ *cobra.Command, args []string) error {
	toolName := args[0]
	toolInput := args[1]

	g, err := loadGraph(filepath.Join(LocalDataDir(), "graph.json"))
	if err != nil {
		return nil
	}

	symbol := extractSymbolFromToolInput(toolName, toolInput)
	if symbol == "" {
		return nil
	}

	res := hookengine.Query(g, symbol)
	if !res.NodeFound {
		return nil
	}

	// Lead with a tag so the agent can recognize and trust the block —
	// the cursor rule references the same "[Universe]" prefix.
	fmt.Printf("[Universe] %s", res.Response)

	// Confidence band determines how strongly we tell the agent to skip
	// the file read. The thresholds match GitNexus's convention: 0.8 is
	// the cutoff for "trust this directly", 0.5 is the floor for "look
	// but verify".
	switch {
	case res.MinConfidence >= 0.8:
		fmt.Printf("Confidence: all relationships ≥%.0f%%\n", res.MinConfidence*100)
		fmt.Println("Graph has complete context. File read is likely unnecessary.")
	case res.MinConfidence >= 0.5:
		fmt.Printf("Confidence: some relationships are %.0f%% (inferred).\n", res.MinConfidence*100)
		fmt.Println("Graph has partial context. Targeted file read may help verify.")
	}
	// minConf < 0.5: don't print a guidance line — let the agent read
	// the file without our recommendation. The graph data is still
	// above, which is itself useful context.
	return nil
}

// extractSymbolFromToolInput pulls a searchable name out of the JSON
// payload Cursor sends to the hook. Different tools use different keys
// (file_path vs path, pattern vs query) so we try a small list per tool.
func extractSymbolFromToolInput(toolName, inputJSON string) string {
	var input map[string]interface{}
	if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
		return ""
	}
	getStr := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := input[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}
	switch toolName {
	case "Read", "ReadFile":
		fp := getStr("file_path", "path")
		if fp == "" {
			return ""
		}
		base := filepath.Base(fp)
		for _, ext := range []string{".go", ".py", ".ts", ".tsx", ".js", ".jsx"} {
			base = strings.TrimSuffix(base, ext)
		}
		return base
	case "Grep", "Search", "RipGrep":
		return getStr("pattern", "query")
	case "Glob", "ListFiles":
		p := getStr("pattern", "path")
		if p == "" {
			return ""
		}
		p = strings.TrimSuffix(p, "/*")
		p = strings.TrimSuffix(p, "/**")
		return filepath.Base(p)
	}
	return ""
}
