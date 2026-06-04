package analyzer

import (
	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/models"
)

// setEdgeConfidence assigns a confidence score to every edge based on
// how reliably it was resolved at parse time. Runs after the graph is
// built so cluster/flow/impact passes can use the scores too.
//
// The scoring is a deliberate floor — calls within the same file have
// to be right (the AST saw them directly), cross-package calls that
// land on an unindexed target (stdlib, vendored, dynamic) are guesses.
func setEdgeConfidence(g *graph.Graph) {
	if g == nil {
		return
	}
	for _, e := range g.Edges {
		if e == nil {
			continue
		}
		switch e.Type {
		case models.EdgeContains, models.EdgeImports:
			e.Confidence = 1.0
			continue
		}
		fromNode := g.Nodes[e.From]
		toNode := g.Nodes[e.To]
		if fromNode == nil || toNode == nil {
			// One end is outside the indexed graph (stdlib, external,
			// or unresolved name) — flag as guess.
			e.Confidence = 0.5
			continue
		}
		switch {
		case fromNode.FilePath == toNode.FilePath && fromNode.FilePath != "":
			e.Confidence = 1.0
		case fromNode.Package == toNode.Package && fromNode.Package != "":
			e.Confidence = 0.9
		case toNode.Package != "" && fromNode.Package != "":
			if hasImportEdge(g, fromNode, toNode) {
				e.Confidence = 0.8
			} else {
				e.Confidence = 0.6
			}
		default:
			e.Confidence = 0.5
		}
	}
}

// hasImportEdge reports whether fromNode's enclosing file imports
// toNode's package. Used to distinguish a resolved cross-package call
// from a guessed one.
func hasImportEdge(g *graph.Graph, from, to *models.Node) bool {
	for _, e := range g.Edges {
		if e == nil || e.Type != models.EdgeImports || e.From != from.ID {
			continue
		}
		target := g.Nodes[e.To]
		if target != nil && target.Package == to.Package {
			return true
		}
	}
	return false
}
