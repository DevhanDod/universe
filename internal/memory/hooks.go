package memory

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SessionManager tracks active agent sessions and captures events.
type SessionManager struct {
	mu         sync.RWMutex
	sessions   map[string]*Session // keyed by developerID
	store      *Store
	compressor *Compressor
	embedder   EmbedFunc
	config     Config
	stopCh     chan struct{}
}

// NewSessionManager creates a new SessionManager and starts the timeout ticker.
func NewSessionManager(store *Store, compressor *Compressor, embedder EmbedFunc, config Config) *SessionManager {
	sm := &SessionManager{
		sessions:   make(map[string]*Session),
		store:      store,
		compressor: compressor,
		embedder:   embedder,
		config:     config,
		stopCh:     make(chan struct{}),
	}
	go sm.runTimeoutChecker()
	return sm
}

// OnToolCall is called every time an MCP tool is invoked.
func (sm *SessionManager) OnToolCall(developerID, toolName, graphNodeID, input, output string, success bool, repoID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, exists := sm.sessions[developerID]
	if !exists {
		session = &Session{
			ID:           uuid.New().String(),
			DeveloperID:  developerID,
			RepoID:       repoID,
			StartedAt:    time.Now(),
			LastActiveAt: time.Now(),
		}
		sm.sessions[developerID] = session
	}

	// Truncate input/output to 500 chars
	if len(input) > 500 {
		input = input[:500]
	}
	if len(output) > 500 {
		output = output[:500]
	}

	event := SessionEvent{
		Timestamp:   time.Now(),
		EventType:   "tool_use",
		ToolName:    toolName,
		GraphNodeID: graphNodeID,
		Input:       input,
		Output:      output,
		Success:     success,
	}
	session.RawEvents = append(session.RawEvents, event)

	if graphNodeID != "" {
		if !containsString(session.TouchedNodes, graphNodeID) {
			session.TouchedNodes = append(session.TouchedNodes, graphNodeID)
		}
	}

	session.LastActiveAt = time.Now()
}

// EndSession explicitly ends a session.
func (sm *SessionManager) EndSession(developerID string) {
	sm.mu.Lock()
	session, exists := sm.sessions[developerID]
	if !exists {
		sm.mu.Unlock()
		return
	}
	delete(sm.sessions, developerID)
	sm.mu.Unlock()

	go sm.processSessionEnd(session)
}

// Stop gracefully shuts down the session manager, ending all active sessions.
func (sm *SessionManager) Stop() {
	close(sm.stopCh)

	sm.mu.Lock()
	active := make([]*Session, 0, len(sm.sessions))
	for _, s := range sm.sessions {
		active = append(active, s)
	}
	sm.sessions = make(map[string]*Session)
	sm.mu.Unlock()

	var wg sync.WaitGroup
	for _, s := range active {
		wg.Add(1)
		s := s
		go func() {
			defer wg.Done()
			sm.processSessionEnd(s)
		}()
	}
	wg.Wait()
}

func (sm *SessionManager) processSessionEnd(session *Session) {
	if len(session.RawEvents) < 2 {
		return
	}

	observations, err := sm.compressor.CompressEvents(
		session.RawEvents, session.DeveloperID, session.RepoID, session.ID,
	)
	if err != nil {
		log.Printf("session %s: compress error: %v", session.ID, err)
		return
	}

	// Compute embeddings
	for i := range observations {
		if sm.embedder != nil {
			text := observations[i].Summary
			if observations[i].Detail != "" {
				text += " " + observations[i].Detail
			}
			embedding, err := sm.embedder(text)
			if err != nil {
				log.Printf("session %s: embed error for obs %d: %v", session.ID, i, err)
				// Store without embedding — still findable by keyword/graph
			} else {
				observations[i].Embedding = embedding
			}
		}
	}

	stored, err := sm.store.InsertBatch(observations)
	if err != nil {
		log.Printf("session %s: insert batch error: %v", session.ID, err)
		return
	}

	log.Printf("session %s ended: %d observations stored for developer %s",
		session.ID, len(stored), session.DeveloperID)
}

func (sm *SessionManager) runTimeoutChecker() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-sm.stopCh:
			return
		case <-ticker.C:
			sm.checkTimeouts()
		}
	}
}

func (sm *SessionManager) checkTimeouts() {
	timeout := time.Duration(sm.config.SessionTimeoutMinutes) * time.Minute
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}

	sm.mu.RLock()
	var timedOut []string
	for devID, session := range sm.sessions {
		if time.Since(session.LastActiveAt) > timeout {
			timedOut = append(timedOut, devID)
		}
	}
	sm.mu.RUnlock()

	for _, devID := range timedOut {
		sm.EndSession(devID)
	}
}

// ActiveSessionCount returns the number of currently active sessions (useful for tests).
func (sm *SessionManager) ActiveSessionCount() int {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return len(sm.sessions)
}

// GetSession returns a copy of the session for a developer (useful for tests).
func (sm *SessionManager) GetSession(developerID string) (*Session, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	s, ok := sm.sessions[developerID]
	if !ok {
		return nil, false
	}
	cp := *s
	return &cp, true
}

func containsString(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

// GraphAdapter wraps your existing graph.Graph to implement GraphQuerier.
// Wire this up during MCP integration.
type GraphAdapter struct {
	getCallerIDs func(nodeID string) ([]string, error)
	getCalleeIDs func(nodeID string) ([]string, error)
}

// NewGraphAdapter creates a GraphAdapter from two edge-lookup functions.
// Pass closures that call your existing graph.Graph methods.
func NewGraphAdapter(
	callerFn func(nodeID string) ([]string, error),
	calleeFn func(nodeID string) ([]string, error),
) GraphQuerier {
	return &GraphAdapter{getCallerIDs: callerFn, getCalleeIDs: calleeFn}
}

func (ga *GraphAdapter) GetCallerIDs(nodeID string) ([]string, error) {
	if ga.getCallerIDs == nil {
		return nil, fmt.Errorf("getCallerIDs not configured")
	}
	return ga.getCallerIDs(nodeID)
}

func (ga *GraphAdapter) GetCalleeIDs(nodeID string) ([]string, error) {
	if ga.getCalleeIDs == nil {
		return nil, fmt.Errorf("getCalleeIDs not configured")
	}
	return ga.getCalleeIDs(nodeID)
}
