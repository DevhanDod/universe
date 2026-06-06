package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/models"
)

// universe session-digest is the only hook still wired up after the
// v0.4.0 cleanup. Cursor's sessionStart hook DOES inject the JSON
// response's `additional_context` field into the model's context — it
// is the one hook channel that survived testing. We use it to drop a
// ~200-token codebase digest (counts + key clusters + entry points)
// at the start of each session so the agent has structural awareness
// even before its first `universe query` call.
//
// PreToolUse / postToolUse hooks were tried (v0.3.x) and confirmed
// non-functional in Cursor; they're gone.

type sessionDigestResponse struct {
	AdditionalContext string `json:"additional_context,omitempty"`
}

var sessionDigestCmd = &cobra.Command{
	Use:    "session-digest",
	Short:  "Emit a compact project digest for Cursor's sessionStart hook",
	Hidden: true,
	RunE:   runSessionDigest,
}

func init() {
	rootCmd.AddCommand(sessionDigestCmd)
}

func runSessionDigest(_ *cobra.Command, _ []string) error {
	// Cursor pipes a JSON payload in on stdin; we don't need any of
	// it. Draining stdin avoids a write-end EPIPE if Cursor checks.
	_, _ = io.Copy(io.Discard, os.Stdin)

	graphPath := filepath.Join(LocalDataDir(), "graph.json")
	g, err := loadGraph(graphPath)
	if err != nil {
		// No graph yet — emit an empty response, NOT an error. A
		// non-zero exit here would block the session from starting.
		return emitDigest("")
	}
	return emitDigest(buildSessionDigest(g))
}

func emitDigest(text string) error {
	enc := json.NewEncoder(os.Stdout)
	return enc.Encode(sessionDigestResponse{AdditionalContext: text})
}

// buildSessionDigest builds the resident-for-session summary. Target
// is 200-300 tokens — anything bigger pays its cost on every turn for
// the rest of the session, which is exactly the problem we're trying
// to dodge with the rest of the v0.4.0 redesign.
func buildSessionDigest(g *graph.Graph) string {
	if g == nil {
		return ""
	}
	stats := g.Stats()
	var b strings.Builder

	fmt.Fprintf(&b, "[Universe] %d symbols, %d files, %d packages\n",
		stats.TotalNodes, stats.FileCount, stats.PackageCount)

	if names := topClusterNames(g.Clusters, 5); len(names) > 0 {
		fmt.Fprintf(&b, "Key clusters: %s\n", strings.Join(names, ", "))
	}

	if entries := topEntryPoints(g, 5); len(entries) > 0 {
		fmt.Fprintf(&b, "Entry points: %s\n", strings.Join(entries, ", "))
	}

	b.WriteString(`Run "universe query <name>" in the terminal for graph context on any symbol.`)
	return b.String()
}

// topClusterNames returns up to n cluster names sorted by size desc.
// Format is "name (count)" so the agent has a hint about which cluster
// dominates the codebase.
func topClusterNames(clusters []models.Cluster, n int) []string {
	if len(clusters) == 0 {
		return nil
	}
	cp := make([]models.Cluster, len(clusters))
	copy(cp, clusters)
	sort.Slice(cp, func(i, j int) bool { return cp[i].NodeCount > cp[j].NodeCount })
	if n > len(cp) {
		n = len(cp)
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf("%s (%d)", cp[i].Name, cp[i].NodeCount))
	}
	return out
}

// topEntryPoints picks function nodes that look like entry points:
// many flows, no callers, recognizable names (main / Test* / Handle*).
// Falls back to nodes with the most flows when no clear entries exist.
func topEntryPoints(g *graph.Graph, n int) []string {
	type ranked struct {
		name  string
		flows int
	}
	var candidates []ranked
	for _, node := range g.Nodes {
		if node == nil {
			continue
		}
		if node.Type != models.NodeFunction && node.Type != models.NodeMethod {
			continue
		}
		if len(node.Flows) == 0 {
			continue
		}
		if !isLikelyEntryPoint(node.Name) {
			continue
		}
		candidates = append(candidates, ranked{node.Name, len(node.Flows)})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].flows != candidates[j].flows {
			return candidates[i].flows > candidates[j].flows
		}
		return candidates[i].name < candidates[j].name
	})
	if n > len(candidates) {
		n = len(candidates)
	}
	out := make([]string, 0, n)
	seen := map[string]struct{}{}
	for _, c := range candidates {
		if _, dup := seen[c.name]; dup {
			continue
		}
		seen[c.name] = struct{}{}
		out = append(out, c.name)
		if len(out) >= n {
			break
		}
	}
	return out
}

func isLikelyEntryPoint(name string) bool {
	switch {
	case name == "main", name == "init":
		return true
	case strings.HasPrefix(name, "Test"), strings.HasPrefix(name, "Benchmark"):
		return true
	case strings.HasPrefix(name, "Handle"), strings.HasSuffix(name, "Handler"):
		return true
	case strings.HasSuffix(name, "Cmd"), strings.HasSuffix(name, "Command"):
		return true
	}
	return false
}
