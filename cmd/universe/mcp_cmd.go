package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// v0.3.0 removed the MCP server. v0.4.0 also dropped the PreToolUse
// hook (Cursor doesn't honor stdout from preToolUse). Universe now
// works via .cursor/rules/universe.mdc + the shell CLI — the agent
// runs `universe query <name>` as a Shell tool call. This stub stays
// so projects with a stale `universe mcp --repo .` in .cursor/mcp.json
// see a clear next step rather than a connection failure.

var mcpRepoPath string

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "(deprecated) Universe no longer ships an MCP server",
	RunE:  runMCPDeprecated,
}

func init() {
	mcpCmd.Flags().StringVar(&mcpRepoPath, "repo", "", "(ignored)")
}

func runMCPDeprecated(_ *cobra.Command, _ []string) error {
	fmt.Fprintln(os.Stderr, "universe mcp: removed in v0.4.0.")
	fmt.Fprintln(os.Stderr, "Universe now works through .cursor/rules/universe.mdc plus")
	fmt.Fprintln(os.Stderr, "shell commands (universe query / search / impact / deps).")
	fmt.Fprintln(os.Stderr, "Run `universe init` to set up the rule, then remove the")
	fmt.Fprintln(os.Stderr, "\"universe\" entry from .cursor/mcp.json if you have one.")
	// Exit non-zero so Cursor's MCP client logs a clear failure rather
	// than reporting "server connected but offers 0 tools".
	os.Exit(2)
	return nil
}
