package orchestrator

import "fmt"

// Orchestrator is Engine 5 — plan bridge + cost tracker + workspace generator.
// It does NOT call any LLM APIs. Cursor does the AI work.
type Orchestrator struct {
	router    *Router
	planStore *PlanStore
	tracker   *Tracker
	config    Config
}

// New creates a fully wired orchestrator.
// skills, memory, and graph may be nil — they are used only for recommendations.
func New(
	skills SkillChecker,
	memory MemoryChecker,
	graph GraphChecker,
	config Config,
) (*Orchestrator, error) {
	router := NewRouter(skills, memory, graph)

	var planStore *PlanStore
	var tracker *Tracker
	var err error

	if config.DatabaseURL != "" {
		planStore, err = NewPlanStore(config.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("plan store: %w", err)
		}
		tracker, err = NewTracker(config.DatabaseURL)
		if err != nil {
			planStore.Close() //nolint:errcheck
			return nil, fmt.Errorf("tracker: %w", err)
		}
	}

	return &Orchestrator{
		router:    router,
		planStore: planStore,
		tracker:   tracker,
		config:    config,
	}, nil
}

func (o *Orchestrator) Router() *Router       { return o.router }
func (o *Orchestrator) PlanStore() *PlanStore { return o.planStore }
func (o *Orchestrator) Tracker() *Tracker     { return o.tracker }
func (o *Orchestrator) Config() Config        { return o.config }

// Stop closes all database connections.
func (o *Orchestrator) Stop() {
	if o.planStore != nil {
		o.planStore.Close() //nolint:errcheck
	}
	if o.tracker != nil {
		o.tracker.Close()
	}
}
