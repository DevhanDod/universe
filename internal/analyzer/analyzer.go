package analyzer

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/Universe/universe/internal/extractor"
	"github.com/Universe/universe/internal/graph"
	"github.com/Universe/universe/internal/models"
	"github.com/Universe/universe/internal/parser"
	"github.com/Universe/universe/internal/scanner"
)

type Config struct {
	Verbose       bool
	Concurrency   int
	IncludeSource bool
}

type Analyzer struct {
	scanner       *scanner.Scanner
	registry      *parser.Registry
	graph         *graph.Graph
	extractors    []extractor.Extractor
	verbose       bool
	concurrency   int
	includeSource bool
}

func NewAnalyzer(registry *parser.Registry, extractors []extractor.Extractor, cfg Config) *Analyzer {
	w := cfg.Concurrency
	if w <= 0 {
		w = runtime.NumCPU()
	}
	return &Analyzer{
		scanner:       scanner.NewScanner(registry),
		registry:      registry,
		graph:         graph.NewGraph(),
		extractors:    extractors,
		verbose:       cfg.Verbose,
		concurrency:   w,
		includeSource: cfg.IncludeSource,
	}
}

func edgeDedupKey(e *models.Edge) string {
	if e == nil {
		return ""
	}
	return string(e.Type) + "\x00" + e.From + "\x00" + e.To
}

func mergeParseResultIntoGraph(g *graph.Graph, pr *models.ParseResult, seenEdges map[string]struct{}) {
	if g == nil || pr == nil {
		return
	}
	for i := range pr.Nodes {
		n := pr.Nodes[i]
		g.AddNode(&n)
	}
	for i := range pr.Edges {
		e := &pr.Edges[i]
		key := edgeDedupKey(e)
		if key == "" {
			continue
		}
		if _, dup := seenEdges[key]; dup {
			continue
		}
		seenEdges[key] = struct{}{}
		g.AddEdge(e)
	}
}

// Analyze runs the full pipeline on a project path: scan → parse → extract → graph
func (a *Analyzer) Analyze(projectPath string) (*graph.Graph, error) {
	a.graph = graph.NewGraph()
	seenEdges := make(map[string]struct{})

	files, err := a.scanner.Scan(projectPath)
	if err != nil {
		return nil, fmt.Errorf("scan: %w", err)
	}
	found := len(files)

	jobs := make(chan scanner.ScannedFile)
	var wg sync.WaitGroup

	var (
		parseMu   sync.Mutex
		parseErrs int
		results   []*models.ParseResult
		fileInfos []*models.FileInfo
	)

	workers := a.concurrency
	if workers < 1 {
		workers = 1
	}

	includeSource := a.includeSource

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				if a.verbose {
					fmt.Fprintf(os.Stderr, "Parsing %s...\n", f.Path)
				}

				content, readErr := os.ReadFile(f.Path)
				if readErr != nil {
					parseMu.Lock()
					parseErrs++
					parseMu.Unlock()
					log.Printf("analyzer: read %s: %v", f.Path, readErr)
					continue
				}

				p := a.registry.GetParser(f.Extension)
				if p == nil {
					// No structural parser — file still gets a NodeFile later
					// in buildFileInventory. Skip parsing, don't log as error.
					continue
				}

				pr, parseErr := p.Parse(f.Path, content)
				if parseErr != nil {
					parseMu.Lock()
					parseErrs++
					parseMu.Unlock()
					log.Printf("analyzer: parse %s: %v", f.Path, parseErr)
					continue
				}
				if pr == nil {
					parseMu.Lock()
					parseErrs++
					parseMu.Unlock()
					log.Printf("analyzer: parse %s: nil result", f.Path)
					continue
				}

				relPath, _ := filepath.Rel(projectPath, f.Path)
				if relPath == "" {
					relPath = f.Path
				}
				relPath = filepath.ToSlash(relPath)

				pr.FilePath = relPath
				for i := range pr.Nodes {
					pr.Nodes[i].FilePath = relPath
				}

				parseMu.Lock()
				results = append(results, pr)
				if includeSource {
					fileInfos = append(fileInfos, &models.FileInfo{
						Path:     relPath,
						Language: f.Language,
						Content:  string(content),
						Lines:    strings.Count(string(content), "\n") + 1,
					})
				}
				parseMu.Unlock()
			}
		}()
	}

	for _, f := range files {
		// Only dispatch files a parser will accept. Non-source files (configs,
		// images, binaries, docs) get a FileNode minted directly in
		// buildFileInventory below — no need to read their bytes.
		if f.Language == "" || f.IsBinary {
			continue
		}
		jobs <- f
	}
	close(jobs)
	wg.Wait()

	parsed := len(results)

	for _, fi := range fileInfos {
		a.graph.AddFile(fi)
	}

	for _, pr := range results {
		mergeParseResultIntoGraph(a.graph, pr, seenEdges)
	}

	for _, pr := range results {
		if pr == nil {
			continue
		}
		for _, ex := range a.extractors {
			if ex == nil {
				continue
			}
			if _, exErr := ex.Extract(pr, results); exErr != nil {
				return nil, fmt.Errorf("extract [%s] %s: %w", ex.Language(), pr.FilePath, exErr)
			}
		}
	}

	for _, pr := range results {
		mergeParseResultIntoGraph(a.graph, pr, seenEdges)
	}

	// Build the file inventory: every file the scanner saw, parser or not.
	// Decorates already-created file nodes with kind/ignored/size, and creates
	// fresh NodeFile entries for everything else (configs, images, binaries,
	// docs). Also synthesises GitignorePattern nodes + Ignores edges.
	a.buildFileInventory(files, projectPath)

	// Coverage = parsed source bytes / total bytes (excluding binaries).
	// Stored on the graph so it shows up in Stats().
	a.graph.SetCoverage(computeCoverage(files))

	// Score edges first so cluster/flow/impact passes (and downstream
	// MCP queries) can filter by confidence.
	setEdgeConfidence(a.graph)

	// Precompute clusters, flows, and impact summaries. These are stored
	// on the graph so MCP tools can answer 360° questions in one call
	// instead of forcing the agent to explore via repeated tool calls.
	// Order matters: clusters first (flows record cluster names per step),
	// then flows (impact uses Node.Flows when assessing relevance), then
	// impact (uses caller counts and flow membership to pick targets).
	a.graph.SetClusters(DetectClusters(a.graph))
	a.graph.SetFlows(DetectFlows(a.graph))
	a.graph.SetImpact(PrecomputeImpact(a.graph))

	if a.verbose {
		log.Printf("analyzer: files found=%d parsed=%d parse errors=%d", found, parsed, parseErrs)
	}
	return a.graph, nil
}

// buildFileInventory ensures every scanned file is represented in the graph,
// tagged with kind/ignored/binary/size so the agent can filter and so the
// coverage metric isn't blind to non-source files.
func (a *Analyzer) buildFileInventory(files []scanner.ScannedFile, projectPath string) {
	// Map relative path -> existing node so we can decorate rather than dup.
	existingByPath := map[string]*models.Node{}
	for _, n := range a.graph.Nodes {
		if n == nil || n.Type != models.NodeFile {
			continue
		}
		existingByPath[n.FilePath] = n
	}

	gitignoreNodes := map[string]string{} // source -> nodeID

	for _, f := range files {
		relPath, _ := filepath.Rel(projectPath, f.Path)
		if relPath == "" {
			relPath = f.Path
		}
		relPath = filepath.ToSlash(relPath)

		node := existingByPath[relPath]
		if node == nil {
			// Find by absolute path too — Go parser stores absolute file paths.
			node = existingByPath[filepath.ToSlash(f.Path)]
		}
		if node == nil {
			// Mint a file node for non-source files (configs, images, …).
			id := "file:" + relPath
			node = &models.Node{
				ID:        id,
				Name:      filepath.Base(relPath),
				Type:      models.NodeFile,
				FilePath:  relPath,
				StartLine: 1,
				EndLine:   1,
				Metadata:  map[string]string{},
			}
			a.graph.AddNode(node)
		}
		if node.Metadata == nil {
			node.Metadata = map[string]string{}
		}
		node.Metadata["kind"] = f.Kind
		node.Metadata["size"] = strconv.FormatInt(f.Size, 10)
		if f.IsBinary {
			node.Metadata["is_binary"] = "true"
		}
		if f.IsGenerated {
			node.Metadata["is_generated"] = "true"
		}
		if f.IsIgnored {
			node.Metadata["is_ignored"] = "true"
			node.Metadata["ignored_by"] = f.IgnoredBy

			// One GitignorePattern node per distinct source, plus an Ignores edge.
			// Lets the agent ask "what does .gitignore drop?" with one query.
			patternID, ok := gitignoreNodes[f.IgnoredBy]
			if !ok {
				patternID = "gitignore:" + f.IgnoredBy
				gitignoreNodes[f.IgnoredBy] = patternID
				a.graph.AddNode(&models.Node{
					ID:       patternID,
					Name:     f.IgnoredBy,
					Type:     models.NodeFile, // reuse — no NodePattern type today
					FilePath: f.IgnoredBy,
					Metadata: map[string]string{"kind": "gitignore_pattern"},
				})
			}
			a.graph.AddEdge(&models.Edge{
				From: patternID,
				To:   node.ID,
				Type: models.EdgeIgnores,
			})
		}
	}
}

// computeCoverage produces a multi-angle view of how much of the codebase the
// graph actually understands. File-only counts lie (one giant unparsed file
// equals a tiny .gitignore), so we report byte coverage too and break it down
// by kind.
func computeCoverage(files []scanner.ScannedFile) models.Coverage {
	cov := models.Coverage{
		Breakdown: map[string]models.CoverageBucket{},
	}
	for _, f := range files {
		cov.TotalFiles++
		cov.TotalBytes += f.Size

		b := cov.Breakdown[f.Kind]
		b.Files++
		b.Bytes += f.Size

		if f.Language != "" && !f.IsBinary {
			cov.ParsedFiles++
			cov.ParsedBytes += f.Size
			b.FilesParsed++
			b.BytesParsed += f.Size
		}
		cov.Breakdown[f.Kind] = b
	}
	if cov.TotalFiles > 0 {
		cov.FileCoverage = float64(cov.ParsedFiles) / float64(cov.TotalFiles)
	}
	if cov.TotalBytes > 0 {
		cov.ByteCoverage = float64(cov.ParsedBytes) / float64(cov.TotalBytes)
	}
	return cov
}

func (a *Analyzer) Graph() *graph.Graph {
	return a.graph
}
