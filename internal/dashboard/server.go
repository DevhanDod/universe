package dashboard

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os/exec"
	"runtime"
	"time"

	"github.com/Universe/universe/internal/graph"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed static
var staticFiles embed.FS

// Server is the dashboard HTTP server.
type Server struct {
	db         *pgxpool.Pool
	graph      *graph.Graph
	projectDir string
	port       int
	srv        *http.Server
}

// NewServer creates a dashboard server. db and g may be nil (graceful degradation).
// projectDir is the path to the project root (where .universe/graph.json lives).
func NewServer(databaseURL string, port int, projectDir string, g *graph.Graph) (*Server, error) {
	s := &Server{port: port, graph: g, projectDir: projectDir}

	if databaseURL != "" {
		pool, err := pgxpool.New(context.Background(), databaseURL)
		if err != nil {
			log.Printf("dashboard: DB connect failed (%v) — running without database", err)
		} else if err := pool.Ping(context.Background()); err != nil {
			pool.Close()
			log.Printf("dashboard: DB ping failed (%v) — running without database", err)
		} else {
			s.db = pool
			log.Printf("dashboard: DB connected")
		}
	}
	return s, nil
}

// Start registers all routes and begins listening. Blocks until the server stops.
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// ── API routes ──────────────────────────────────────────────────────────
	mux.HandleFunc("/api/overview", corsMiddleware(s.HandleOverview))

	mux.HandleFunc("/api/memory/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// /api/memory/:id
		if len(r.URL.Path) > len("/api/memory/") {
			s.HandleMemoryDetail(w, r)
		} else {
			s.HandleMemoryList(w, r)
		}
	}))
	mux.HandleFunc("/api/memory", corsMiddleware(s.HandleMemoryList))

	mux.HandleFunc("/api/skills/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		// /api/skills/:id/lineage  or  /api/skills/:id
		if hasSegment(r.URL.Path, "lineage") {
			s.HandleSkillLineage(w, r)
		} else {
			s.HandleSkillDetail(w, r)
		}
	}))
	mux.HandleFunc("/api/skills", corsMiddleware(s.HandleSkillsList))

	mux.HandleFunc("/api/compression/samples", corsMiddleware(s.HandleCompressionSamples))

	mux.HandleFunc("/api/routing/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) > len("/api/routing/") {
			s.HandleRoutingDetail(w, r)
		} else {
			s.HandleRoutingList(w, r)
		}
	}))
	mux.HandleFunc("/api/routing", corsMiddleware(s.HandleRoutingList))

	mux.HandleFunc("/api/plans/stats", corsMiddleware(s.HandlePlanStats))
	mux.HandleFunc("/api/plans/", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) > len("/api/plans/") {
			s.HandlePlanDetail(w, r)
		} else {
			s.HandlePlansList(w, r)
		}
	}))
	mux.HandleFunc("/api/plans", corsMiddleware(s.HandlePlansList))

	mux.HandleFunc("/api/graph/nodes", corsMiddleware(s.HandleGraphNodes))
	mux.HandleFunc("/api/graph/edges", corsMiddleware(s.HandleGraphEdges))
	mux.HandleFunc("/api/graph/node/", corsMiddleware(s.HandleGraphNodeDetail))

	// ── Static SPA ──────────────────────────────────────────────────────────
	staticRoot, err := fs.Sub(staticFiles, "static")
	if err != nil {
		return fmt.Errorf("static fs: %w", err)
	}
	fileServer := http.FileServer(http.FS(staticRoot))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// For any non-API path, serve index.html (SPA routing)
		if r.URL.Path != "/" {
			// Try to serve the file; if not found, serve index.html
			if _, err := fs.Stat(staticRoot, r.URL.Path[1:]); err != nil {
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/"
				fileServer.ServeHTTP(w, r2)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	})

	s.srv = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	log.Printf("dashboard: listening on http://localhost:%d", s.port)
	return s.srv.ListenAndServe()
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	if s.db != nil {
		s.db.Close()
	}
	if s.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.srv.Shutdown(ctx)
	}
	return nil
}

// OpenBrowser opens the given URL in the system browser.
func OpenBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	if err := exec.Command(cmd, args...).Start(); err != nil {
		log.Printf("dashboard: could not open browser: %v", err)
	}
}

// ── middleware ────────────────────────────────────────────────────────────────

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:5173") // Vite dev
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func hasSegment(path, segment string) bool {
	parts := splitPath(path)
	for _, p := range parts {
		if p == segment {
			return true
		}
	}
	return false
}

func splitPath(path string) []string {
	var out []string
	for _, p := range splitSlash(path) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func splitSlash(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}
