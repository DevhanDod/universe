package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Universe/universe/internal/models"
	"github.com/Universe/universe/internal/skills"
)

// universe_context — primary MCP tool. One call combines graph,
// memory, and skill data so the agent doesn't chain 3-5 tools to
// answer "what is X / who calls X / has anyone fixed this before".
//
// Sections render as bare "=== HEADER ===" blocks so a single string
// of plain text covers everything; the agent can stop reading after
// the graph section if memory/skills aren't relevant.

type ContextInputV28 struct {
	Name          string  `json:"name"`
	Question      string  `json:"question,omitempty"`
	MaxDepth      int     `json:"max_depth,omitempty"`
	MinConfidence float64 `json:"min_confidence,omitempty"`
	IncludeMemory bool    `json:"include_memory,omitempty"`
	IncludeSkills bool    `json:"include_skills,omitempty"`
}

func (h *Handlers) HandleContextV28(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input ContextInputV28,
) (*mcp.CallToolResult, TextOutput, error) {
	// Apply defaults. We default the include flags ON because their
	// zero-value (false) would silently disable them — fine for an
	// API but bad for a "primary tool" that should be useful with
	// just a name.
	if input.MaxDepth <= 0 {
		input.MaxDepth = 2
	}
	if input.MinConfidence <= 0 {
		input.MinConfidence = 0.5
	}
	includeMemory := input.IncludeMemory || input.Question == ""
	_ = includeMemory // memory query falls through below; flag kept for parity with spec
	includeSkills := input.IncludeSkills || input.Question != ""

	var sections []string

	// Graph section. If we got an exact ID match use it; otherwise
	// search to suggest the closest hit so the agent doesn't have to
	// re-call universe_search just to disambiguate.
	graphSection, node := h.graphSection(input.Name)
	if graphSection != "" {
		sections = append(sections, "=== GRAPH ===\n"+graphSection)
	}

	// Memory section — skip silently when nothing matches so the
	// response stays small for hot paths with no observations yet.
	if h.memoryStore != nil {
		query := input.Question
		if query == "" {
			query = input.Name
		}
		if rows, err := h.memoryStore.SearchKeyword(query, "", 3); err == nil && len(rows) > 0 {
			var mb strings.Builder
			for _, r := range rows {
				fmt.Fprintf(&mb, "  [%s] %s\n", r.CreatedAt.Format("Jan 2"), r.Summary)
			}
			sections = append(sections, "=== MEMORY ===\n"+mb.String())
		}
	}

	// Skill section — only when the caller looks like they're
	// describing a task (question set, or include_skills true).
	if includeSkills && h.skillMatcher != nil {
		query := input.Question
		if query == "" {
			query = input.Name
		}
		res, err := h.skillMatcher.Match(skills.MatchQuery{TaskText: query, Limit: 1})
		if err == nil && res != nil && res.BestMatch != nil {
			s := res.BestMatch
			line := fmt.Sprintf("  %s v%d — %.0f%% success",
				s.Name, s.Version, s.SuccessRate*100)
			if s.RequiresVerification {
				line += " (requires verification)"
			}
			sections = append(sections, "=== SKILL ===\n"+line+"\n")
		}
	}

	if len(sections) == 0 {
		return nil, TextOutput{
			Text: fmt.Sprintf("No graph / memory / skill data for %q. Try universe_search.", input.Name),
		}, nil
	}
	// Final size guard — total response budget is generous (1200 tok)
	// but truncate the tail rather than silently overshoot.
	text := strings.Join(sections, "\n")
	text = truncateToBudget(text, 1200)
	_ = node
	return nil, TextOutput{Text: text}, nil
}

// graphSection looks up `name` and returns the formatted block plus
// the resolved node (for callers that want to log which symbol won
// the disambiguation).
func (h *Handlers) graphSection(name string) (string, *models.Node) {
	if h.graph == nil {
		return "", nil
	}
	node := h.graph.GetNode(name)
	if node == nil {
		matches := h.graph.SearchNodes(name, 5)
		for _, m := range matches {
			if strings.EqualFold(m.Name, name) {
				node = m
				break
			}
		}
		if node == nil && len(matches) > 0 {
			node = matches[0]
		}
		if node == nil {
			return "", nil
		}
	}
	return FormatNodeResponse(node, h.graph, DefaultResponseConfig()), node
}
