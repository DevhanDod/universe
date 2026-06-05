package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// v0.3.0 removed the MCP server entirely. Universe now provides graph
// access through Cursor's PreToolUse hook (.cursor/hooks.json calls
// `universe hook-check`), not via MCP tool registrations. This stub
// stays so projects with `universe mcp --repo .` in their existing
// .cursor/mcp.json see a clear message instead of a panic; Cursor will
// note the server died, the agent will see no tools registered for
// "universe", and that is the intended state.

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
	fmt.Fprintln(os.Stderr, "universe mcp: removed in v0.3.0.")
	fmt.Fprintln(os.Stderr, "Universe now delivers graph context through Cursor's PreToolUse")
	fmt.Fprintln(os.Stderr, "hook. Run `universe init` to write .cursor/hooks.json, then")
	fmt.Fprintln(os.Stderr, "remove the \"universe\" entry from .cursor/mcp.json if you")
	fmt.Fprintln(os.Stderr, "have one.")
	// Exit non-zero so Cursor's MCP client logs a clear failure rather
	// than reporting "server connected but offers 0 tools".
	os.Exit(2)
	return nil
}
