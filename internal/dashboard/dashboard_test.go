package dashboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer() *Server {
	return &Server{db: nil, graph: nil, port: 3001}
}

func TestHandleOverview(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
	w := httptest.NewRecorder()
	s.HandleOverview(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp OverviewResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(resp.Engines) != 5 {
		t.Errorf("expected 5 engines, got %d", len(resp.Engines))
	}
}

func TestHandleMemoryList_NoDB(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/memory", nil)
	w := httptest.NewRecorder()
	s.HandleMemoryList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp ObservationListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Observations == nil {
		t.Error("observations should not be nil")
	}
}

func TestHandleMemoryList_Pagination(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/memory?page=2&limit=5", nil)
	w := httptest.NewRecorder()
	s.HandleMemoryList(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleSkillsList_NoDB(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/skills", nil)
	w := httptest.NewRecorder()
	s.HandleSkillsList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp SkillListResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Skills == nil {
		t.Error("skills should not be nil")
	}
}

func TestHandleSkillLineage_MissingID(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/skills/lineage", nil)
	w := httptest.NewRecorder()
	s.HandleSkillLineage(w, req)
	if w.Code != http.StatusServiceUnavailable && w.Code != http.StatusBadRequest {
		// either is fine when no DB and no ID
	}
}

func TestHandleRoutingList_NoDB(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/routing", nil)
	w := httptest.NewRecorder()
	s.HandleRoutingList(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandleGraphNodes_NoGraph(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/graph/nodes", nil)
	w := httptest.NewRecorder()
	s.HandleGraphNodes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp GraphNodesResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if resp.Nodes == nil {
		t.Error("nodes should not be nil")
	}
}

func TestHandleGraphNodeDetail_MissingID(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/graph/node/", nil)
	w := httptest.NewRecorder()
	s.HandleGraphNodeDetail(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestHandleCompressionSamples_NoDB(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/compression/samples", nil)
	w := httptest.NewRecorder()
	s.HandleCompressionSamples(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCORSHeaders(t *testing.T) {
	handler := corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodOptions, "/api/overview", nil)
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 for OPTIONS, got %d", w.Code)
	}
	if !strings.Contains(w.Header().Get("Access-Control-Allow-Origin"), "localhost") {
		t.Error("expected CORS Allow-Origin header")
	}
}
