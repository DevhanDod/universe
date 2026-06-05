package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/hookengine"
)

// universe hook-check — invoked by .cursor/hooks.json before the agent
// runs Read/Grep/Search-style tools. The exit channel for talking back
// to Cursor is a JSON document on stdout matching its PreToolUse hook
// protocol; plain text is silently ignored, which is why earlier hook
// versions appeared to "do nothing" inside the chat.
//
// Response schema (Cursor PreToolUse):
//
//   {
//     "permission":    "allow" | "deny",
//     "user_message":  "<shown to the developer; optional>",
//     "agent_message": "<injected into the agent's context; optional>"
//   }
//
// Decision logic, by graph state:
//
//   no graph / no symbol / no match     → permission=allow, empty body
//   graph hit, min confidence >= 0.8    → permission=deny, agent_message=full block
//   graph hit, min confidence  < 0.8    → permission=allow, agent_message=full block + hint
//
// The deny path is the win: the file read never happens, no convention
// rules auto-attach, and the agent has the graph answer in its context.
// The allow-with-context path is a softer assist for cross-package
// relationships we don't fully trust.

type hookResponse struct {
	Permission   string `json:"permission"`
	UserMessage  string `json:"user_message,omitempty"`
	AgentMessage string `json:"agent_message,omitempty"`
}

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

	// Default response: allow the tool through, say nothing. We fall
	// back to this whenever the graph isn't available, the input is
	// unparseable, or no matching symbol exists. The agent's normal
	// Read/Grep path proceeds untouched.
	allow := hookResponse{Permission: "allow"}

	g, err := loadGraph(filepath.Join(LocalDataDir(), "graph.json"))
	if err != nil {
		return emitJSON(allow)
	}
	symbol := extractSymbolFromToolInput(toolName, toolInput)
	if symbol == "" {
		return emitJSON(allow)
	}
	res := hookengine.Query(g, symbol)
	if !res.NodeFound {
		return emitJSON(allow)
	}

	// Format the graph block once; both deny and soft-allow include it
	// as agent_message. Leading "[Universe] " tag matches the cursor
	// rule's example so the agent recognizes the source.
	block := "[Universe] " + strings.TrimRight(res.Response, "\n")

	switch {
	case res.MinConfidence >= 0.8:
		// High-trust path: block the read. Include the exact line range
		// so the agent has a precise escape hatch — it can choose to
		// override by reading the narrow slice rather than the file.
		lineHint := ""
		if res.StartLine > 0 && res.EndLine >= res.StartLine {
			lineHint = fmt.Sprintf(
				"\n\nIf the source body is essential, read ONLY %s lines %d-%d.",
				res.FilePath, res.StartLine, res.EndLine)
		}
		agentMsg := block +
			fmt.Sprintf("\nConfidence: all relationships ≥%.0f%%\n", res.MinConfidence*100) +
			"Graph has complete context. File read blocked — graph already answered." +
			lineHint
		return emitJSON(hookResponse{
			Permission:   "deny",
			UserMessage:  fmt.Sprintf("Universe graph answered %q — file read skipped.", symbol),
			AgentMessage: agentMsg,
		})

	case res.MinConfidence >= 0.5:
		// Medium-trust path: give the agent the data but let the tool
		// run too. The convention-rule cost still applies but at least
		// the agent has structural context as it reads.
		agentMsg := block +
			fmt.Sprintf("\nConfidence: some relationships are %.0f%% (inferred). ",
				res.MinConfidence*100) +
			"Targeted file read may help verify."
		return emitJSON(hookResponse{
			Permission:   "allow",
			AgentMessage: agentMsg,
		})

	default:
		// Low confidence — silent allow. The graph has data but the
		// relationships are guesses; surfacing them would mislead.
		return emitJSON(allow)
	}
}

// emitJSON writes the response as a single JSON object on stdout.
// Cursor expects exactly one document, no trailing newline noise; we
// keep it compact and let json.Encoder add the line terminator.
func emitJSON(r hookResponse) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(r)
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
