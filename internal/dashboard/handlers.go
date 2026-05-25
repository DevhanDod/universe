package dashboard

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func queryParam(r *http.Request, key string) string {
	return strings.TrimSpace(r.URL.Query().Get(key))
}

func pageParam(r *http.Request) (page, limit int) {
	page, _ = strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ = strconv.Atoi(r.URL.Query().Get("limit"))
	if page <= 0 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	return
}

func dateParam(r *http.Request, key string) time.Time {
	s := queryParam(r, key)
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func pathSegment(r *http.Request, index int) string {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if index < len(parts) {
		return parts[index]
	}
	return ""
}

// ── Overview ──────────────────────────────────────────────────────────────────

func (s *Server) HandleOverview(w http.ResponseWriter, r *http.Request) {
	engines, _ := QueryEngineStats(s.db, s.graph)
	monthly, _ := QueryMonthlySummary(s.db)
	trend, _ := QueryMonthlyTrend(s.db)

	if trend == nil {
		trend = []MonthlyDataPoint{}
	}
	writeJSON(w, http.StatusOK, OverviewResponse{
		Engines: engines,
		Monthly: monthly,
		Trend:   trend,
	})
}

// ── Memory ────────────────────────────────────────────────────────────────────

func (s *Server) HandleMemoryList(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		writeJSON(w, http.StatusOK, ObservationListResponse{
			Observations:   []ObservationRow{},
			FiltersApplied: map[string]string{},
		})
		return
	}
	page, limit := pageParam(r)
	result, err := QueryObservations(s.db, ObservationFilters{
		DeveloperID: queryParam(r, "developer"),
		Category:    queryParam(r, "category"),
		GraphNodeID: queryParam(r, "graph_node"),
		RepoID:      queryParam(r, "repo"),
		From:        dateParam(r, "from"),
		To:          dateParam(r, "to"),
		Page:        page,
		Limit:       limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) HandleMemoryDetail(w http.ResponseWriter, r *http.Request) {
	id := pathSegment(r, 2) // /api/memory/:id
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "no database")
		return
	}
	obs, err := QueryObservationDetail(s.db, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "observation not found")
		return
	}
	writeJSON(w, http.StatusOK, obs)
}

// ── Skills ────────────────────────────────────────────────────────────────────

func (s *Server) HandleSkillsList(w http.ResponseWriter, r *http.Request) {
	result, err := QuerySkills(s.db, SkillFilters{
		Language:    queryParam(r, "language"),
		Sort:        queryParam(r, "sort"),
		GraphNodeID: queryParam(r, "graph_node"),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) HandleSkillDetail(w http.ResponseWriter, r *http.Request) {
	id := pathSegment(r, 2) // /api/skills/:id
	if id == "" || id == "lineage" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "no database")
		return
	}
	skill, err := QuerySkillDetail(s.db, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "skill not found")
		return
	}
	writeJSON(w, http.StatusOK, skill)
}

func (s *Server) HandleSkillLineage(w http.ResponseWriter, r *http.Request) {
	// /api/skills/:id/lineage → segment 2 is the id
	id := pathSegment(r, 2)
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing id")
		return
	}
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "no database")
		return
	}
	lineage, err := QuerySkillLineage(s.db, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, lineage)
}

// ── Compression ───────────────────────────────────────────────────────────────

func (s *Server) HandleCompressionSamples(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(queryParam(r, "limit"))
	if limit <= 0 {
		limit = 10
	}
	result, err := QueryCompressionSamples(s.db, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// ── Routing ───────────────────────────────────────────────────────────────────

func (s *Server) HandleRoutingList(w http.ResponseWriter, r *http.Request) {
	page, limit := pageParam(r)
	result, err := QueryRoutingTasks(s.db, RoutingFilters{
		DeveloperID: queryParam(r, "developer"),
		RoutingMode: queryParam(r, "routing_mode"),
		From:        dateParam(r, "from"),
		To:          dateParam(r, "to"),
		Page:        page,
		Limit:       limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) HandleRoutingDetail(w http.ResponseWriter, r *http.Request) {
	taskID := pathSegment(r, 2) // /api/routing/:taskId
	if taskID == "" {
		writeError(w, http.StatusBadRequest, "missing task id")
		return
	}
	if s.db == nil {
		writeError(w, http.StatusServiceUnavailable, "no database")
		return
	}
	detail, err := QueryRoutingDetail(s.db, taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, "task not found")
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

// ── Graph ─────────────────────────────────────────────────────────────────────

func (s *Server) HandleGraphNodes(w http.ResponseWriter, r *http.Request) {
	result, err := QueryGraphNodesWithBadges(s.db, s.graph)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) HandleGraphEdges(w http.ResponseWriter, r *http.Request) {
	if s.graph == nil {
		writeJSON(w, http.StatusOK, GraphEdgesResponse{Edges: []GraphEdgeRow{}})
		return
	}
	edges := make([]GraphEdgeRow, 0, len(s.graph.Edges))
	for _, e := range s.graph.Edges {
		if e == nil {
			continue
		}
		edges = append(edges, GraphEdgeRow{
			From: e.From,
			To:   e.To,
			Type: string(e.Type),
		})
	}
	writeJSON(w, http.StatusOK, GraphEdgesResponse{Edges: edges})
}

func (s *Server) HandleGraphNodeDetail(w http.ResponseWriter, r *http.Request) {
	nodeID := pathSegment(r, 3) // /api/graph/node/:id
	if nodeID == "" {
		writeError(w, http.StatusBadRequest, "missing node id")
		return
	}
	detail, err := QueryGraphNodeDetail(s.db, s.graph, nodeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, detail)
}
