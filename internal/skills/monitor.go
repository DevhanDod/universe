package skills

import (
	"log"
	"time"
)

// Monitor watches skill health metrics and triggers evolution when quality degrades.
type Monitor struct {
	store   *Store
	evolver *Evolver
	config  Config
	stopCh  chan struct{}
}

// NewMonitor creates a new Monitor.
func NewMonitor(store *Store, evolver *Evolver, config Config) *Monitor {
	return &Monitor{
		store:   store,
		evolver: evolver,
		config:  config,
		stopCh:  make(chan struct{}),
	}
}

// CheckSkillHealth evaluates a skill after each execution and triggers FIX if needed.
func (m *Monitor) CheckSkillHealth(skillID string, latestExecution SkillExecution) {
	if latestExecution.Success {
		return
	}

	consecutive, err := m.store.GetConsecutiveFailures(skillID)
	if err != nil {
		log.Printf("monitor: get consecutive failures for %s: %v", skillID, err)
		return
	}
	if consecutive >= m.config.ConsecutiveFailuresForFix {
		m.triggerFix(skillID, latestExecution, "consecutive failures")
		return
	}

	rate, err := m.store.GetRecentFailureRate(skillID, 10)
	if err != nil {
		log.Printf("monitor: get failure rate for %s: %v", skillID, err)
		return
	}
	if rate >= m.config.FailureRateForFix {
		m.triggerFix(skillID, latestExecution, "failure rate threshold")
	}
}

func (m *Monitor) triggerFix(skillID string, exec SkillExecution, reason string) {
	skill, err := m.store.GetByID(skillID)
	if err != nil || skill == nil {
		return
	}
	req := EvolutionRequest{
		AppliedSkill: skill,
		Execution:    exec,
	}
	result, err := m.evolver.tryFixSkill(skill, req)
	if err != nil {
		log.Printf("monitor: fix skill %s failed: %v", skillID, err)
		return
	}
	log.Printf("monitor: triggered FIX for skill %s (%s): %s → %s", skillID, reason, result.Action, result.Reason)
}

// RunDailyMaintenance performs scheduled maintenance tasks.
func (m *Monitor) RunDailyMaintenance() {
	if err := m.store.ResetDailyEvolutionAttempts(); err != nil {
		log.Printf("monitor: reset daily attempts: %v", err)
	}

	pruned, err := m.store.PruneUnusedSkills(m.config.PruneAfterDays)
	if err != nil {
		log.Printf("monitor: prune skills: %v", err)
	} else if pruned > 0 {
		log.Printf("monitor: pruned %d unused skills", pruned)
	}

	if stats, err := m.store.GetStats(); err == nil {
		log.Printf("monitor: skills stats — active:%d frozen:%d executions:%d avg_confidence:%.2f avg_success:%.2f",
			stats.TotalActive, stats.TotalFrozen, stats.TotalExecutions,
			stats.AvgConfidence, stats.AvgSuccessRate)
	}
}

// StartSchedule starts the daily maintenance on a background timer.
func (m *Monitor) StartSchedule() {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.RunDailyMaintenance()
			}
		}
	}()
}

// Stop stops the background schedule.
func (m *Monitor) Stop() {
	close(m.stopCh)
}
