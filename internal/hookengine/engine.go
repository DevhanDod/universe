package hookengine

import (
	"github.com/Universe/universe/internal/formatter"
	"github.com/Universe/universe/internal/graph"
)

// hookengine is the shared "graph lookup + format" pipeline used by
// both the PreToolUse hook (cmd/universe/hook_cmd.go) and the on-demand
// shell commands (cmd/universe/intel.go's universe query). Putting it
// here means the hook and the explicit command can't drift in format.

// QueryResult captures everything a caller needs to render a hook
// response: the formatted text, the minimum confidence across direct
// relationships (so the hook can grade its trust message), and basic
// location metadata for the agent's follow-up navigation.
type QueryResult struct {
	Response      string
	MinConfidence float64
	NodeFound     bool
	NodeName      string
	FilePath      string
	StartLine     int
	EndLine       int
}

// Query looks up `symbol` in the graph and returns a QueryResult. The
// caller decides what to do with NodeFound=false (the hook stays
// silent; an explicit shell command prints "no match").
//
// MinConfidence is the floor across direct callers + callees so the
// hook can say "all relationships >= N%" rather than picking a single
// edge to report on. If there are no direct relationships, MinConfidence
// stays at 1.0 — there's nothing to be uncertain about.
func Query(g *graph.Graph, symbol string) QueryResult {
	if g == nil {
		return QueryResult{NodeFound: false}
	}
	nodes := g.SearchNodes(symbol, 3)
	if len(nodes) == 0 {
		return QueryResult{NodeFound: false}
	}
	first := nodes[0]

	cfg := formatter.DefaultResponseConfig()
	cfg.IncludeConf = true
	response := formatter.FormatNodeResponse(first, g, cfg)

	minConf := 1.0
	for _, c := range g.GetDependentsFiltered(first.ID, 0.0, 1) {
		if c.Confidence < minConf {
			minConf = c.Confidence
		}
	}
	for _, c := range g.GetDependenciesFiltered(first.ID, 0.0, 1) {
		if c.Confidence < minConf {
			minConf = c.Confidence
		}
	}

	return QueryResult{
		Response:      response,
		MinConfidence: minConf,
		NodeFound:     true,
		NodeName:      first.Name,
		FilePath:      first.FilePath,
		StartLine:     first.StartLine,
		EndLine:       first.EndLine,
	}
}
