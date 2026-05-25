package orchestrator

import (
	"sync"
)

// ParallelExecutor runs independent sub-tasks in parallel and
// sequential sub-tasks in dependency order (topological execution).
type ParallelExecutor struct {
	executor   *Executor
	escalation *Escalation
	config     Config
}

func NewParallelExecutor(executor *Executor, escalation *Escalation, config Config) *ParallelExecutor {
	return &ParallelExecutor{executor: executor, escalation: escalation, config: config}
}

// ExecuteAll runs all sub-tasks in a plan with maximum parallelism.
func (pe *ParallelExecutor) ExecuteAll(plan *Plan, taskContext string) ([]SubTaskResult, []EscalationRecord, error) {
	ready, dependsOn, dependedBy := buildDependencyGraph(plan.SubTasks)

	// index sub-tasks by ID
	byID := make(map[string]SubTask, len(plan.SubTasks))
	for _, st := range plan.SubTasks {
		byID[st.ID] = st
	}

	var (
		mu          sync.Mutex
		wg          sync.WaitGroup
		results     = make(map[string]SubTaskResult)
		escalations []EscalationRecord
		sem         = make(chan struct{}, max(pe.config.MaxParallelHaikuCalls, 1))

		// queue of tasks ready to run
		readyCh = make(chan string, len(plan.SubTasks))

		// completed signals
		doneCh = make(chan string, len(plan.SubTasks))
	)

	// seed with initially ready tasks
	for _, id := range ready {
		readyCh <- id
	}

	launched := make(map[string]bool)
	total := len(plan.SubTasks)
	completed := 0

	for completed < total {
		select {
		case id := <-readyCh:
			if launched[id] {
				continue
			}
			launched[id] = true
			wg.Add(1)
			sem <- struct{}{}
			go func(taskID string) {
				defer func() { <-sem; wg.Done(); doneCh <- taskID }()

				mu.Lock()
				prevResults := collectPreviousResults(results, dependsOn[taskID])
				mu.Unlock()

				st := byID[taskID]
				result, _ := pe.executor.ExecuteSubTask(st, taskContext, prevResults)

				if !result.Success {
					finalResult, escRecord, _ := pe.escalation.HandleFailure(st, result, taskContext, prevResults)
					mu.Lock()
					if escRecord != nil {
						escalations = append(escalations, *escRecord)
					}
					if finalResult != nil {
						results[taskID] = *finalResult
					} else {
						results[taskID] = SubTaskResult{
							SubTaskID:    taskID,
							Success:      false,
							ErrorMessage: result.ErrorMessage,
							Model:        result.Model,
						}
					}
					mu.Unlock()
				} else {
					mu.Lock()
					results[taskID] = *result
					mu.Unlock()
				}
			}(id)

		case completedID := <-doneCh:
			completed++
			// unlock any sub-tasks that depended on completedID
			for _, dependent := range dependedBy[completedID] {
				deps := dependsOn[dependent]
				allDone := true
				mu.Lock()
				for _, dep := range deps {
					if _, ok := results[dep]; !ok {
						allDone = false
						break
					}
				}
				mu.Unlock()
				if allDone && !launched[dependent] {
					readyCh <- dependent
				}
			}
		}
	}

	// assemble ordered results
	ordered := make([]SubTaskResult, len(plan.SubTasks))
	for i, st := range plan.SubTasks {
		if r, ok := results[st.ID]; ok {
			ordered[i] = r
		} else {
			ordered[i] = SubTaskResult{SubTaskID: st.ID, Success: false, ErrorMessage: "not executed"}
		}
	}

	return ordered, escalations, nil
}

// buildDependencyGraph creates adjacency lists for topological execution.
func buildDependencyGraph(subTasks []SubTask) (ready []string, dependsOn map[string][]string, dependedBy map[string][]string) {
	dependsOn = make(map[string][]string, len(subTasks))
	dependedBy = make(map[string][]string, len(subTasks))

	for _, st := range subTasks {
		dependsOn[st.ID] = st.DependsOn
		for _, dep := range st.DependsOn {
			dependedBy[dep] = append(dependedBy[dep], st.ID)
		}
	}

	for _, st := range subTasks {
		if len(st.DependsOn) == 0 {
			ready = append(ready, st.ID)
		}
	}
	return
}

func collectPreviousResults(results map[string]SubTaskResult, deps []string) []SubTaskResult {
	out := make([]SubTaskResult, 0, len(deps))
	for _, dep := range deps {
		if r, ok := results[dep]; ok {
			out = append(out, r)
		}
	}
	return out
}
