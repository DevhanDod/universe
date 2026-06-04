package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// universe hook-check — invoked by .cursor/hooks.json before the
// agent runs Read/Grep/Search-style tools. If the graph already has
// data for whatever symbol the tool is about to touch, we print a
// short reminder pointing at universe_context.
//
// The hook is silent when:
//   - the graph isn't built yet (universe init never ran)
//   - the target can't be extracted from the tool input
//   - no matching node exists in the graph
//
// Silent on failure is deliberate — a hook that breaks the user's
// tool calls would be worse than no hook at all.

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
		return nil // silent — no graph yet
	}

	symbol := extractSymbolFromToolInput(toolName, toolInput)
	if symbol == "" {
		return nil
	}

	nodes := g.SearchNodes(symbol, 1)
	if len(nodes) == 0 {
		return nil
	}
	first := nodes[0]
	fmt.Printf("[Universe] Graph has data for %q (e.g. %s [%s] %s:%d). "+
		"Consider universe_context(name: %q) instead of %s.\n",
		symbol, first.Name, first.Type, first.FilePath, first.StartLine,
		first.Name, toolName)
	return nil
}

// extractSymbolFromToolInput pulls a searchable name out of the JSON
// payload Cursor sends to the hook. We try a few common keys per tool
// (file_path / pattern / query) because the exact schema varies by
// Cursor version and tool implementation.
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
