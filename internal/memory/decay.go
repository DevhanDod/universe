package memory

import (
	"log"
	"time"
)

// DecayRunner periodically reduces confidence on old observations.
type DecayRunner struct {
	store  *Store
	config Config
	stopCh chan struct{}
}

// NewDecayRunner creates a new DecayRunner.
func NewDecayRunner(store *Store, config Config) *DecayRunner {
	return &DecayRunner{
		store:  store,
		config: config,
		stopCh: make(chan struct{}),
	}
}

// RunDecay performs one decay cycle.
func (d *DecayRunner) RunDecay() (*DecayResult, error) {
	rows, err := d.store.queryDecayRows(d.config.DecayMinConfidence)
	if err != nil {
		return nil, err
	}

	updates := make(map[string]float64, len(rows))
	now := time.Now()

	for _, row := range rows {
		var reference time.Time
		if row.RecalledAt != nil {
			reference = *row.RecalledAt
		} else {
			reference = row.CreatedAt
		}

		daysUnused := now.Sub(reference).Hours() / 24
		newConfidence := row.Confidence - (daysUnused * d.config.DecayRate)
		if newConfidence < d.config.DecayMinConfidence {
			newConfidence = 0
		}
		updates[row.ID] = newConfidence
	}

	if len(updates) > 0 {
		if err := d.store.UpdateConfidenceBatch(updates); err != nil {
			return nil, err
		}
	}

	deleted, err := d.store.DeleteByConfidence(d.config.DecayMinConfidence)
	if err != nil {
		return nil, err
	}

	remaining, err := d.store.countObservations()
	if err != nil {
		return nil, err
	}

	result := &DecayResult{
		Updated:   len(updates),
		Deleted:   deleted,
		Remaining: remaining,
	}
	log.Printf("decay cycle: %d updated, %d deleted, %d remaining", result.Updated, result.Deleted, result.Remaining)
	return result, nil
}

// StartSchedule starts the decay runner on a background schedule.
func (d *DecayRunner) StartSchedule(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-d.stopCh:
				return
			case <-ticker.C:
				if result, err := d.RunDecay(); err != nil {
					log.Printf("decay error: %v", err)
				} else {
					log.Printf("scheduled decay: %d updated, %d deleted, %d remaining",
						result.Updated, result.Deleted, result.Remaining)
				}
			}
		}
	}()
}

// Stop stops the background schedule.
func (d *DecayRunner) Stop() {
	close(d.stopCh)
}
