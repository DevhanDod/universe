package analyzer

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
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
					parseMu.Lock()
					parseErrs++
					parseMu.Unlock()
					log.Printf("analyzer: no parser for extension %s (%s)", f.Extension, f.Path)
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

	if a.verbose {
		log.Printf("analyzer: files found=%d parsed=%d parse errors=%d", found, parsed, parseErrs)
	}
	return a.graph, nil
}

func (a *Analyzer) Graph() *graph.Graph {
	return a.graph
}
