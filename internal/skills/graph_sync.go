package skills

import (
	"fmt"
	"log"
)

// GraphSync listens for graph change events and flags affected skills.
type GraphSync struct {
	store *Store
}

// NewGraphSync creates a new GraphSync handler.
func NewGraphSync(store *Store) *GraphSync {
	return &GraphSync{store: store}
}

// OnGraphChange processes a graph change event, flagging affected skills.
func (gs *GraphSync) OnGraphChange(event GraphChangeEvent) error {
	if len(event.ChangedNodeIDs) == 0 {
		return nil
	}

	// Flag all skills covering changed nodes as stale
	if err := gs.store.MarkGraphNodesStale(event.ChangedNodeIDs); err != nil {
		return fmt.Errorf("mark stale: %w", err)
	}

	// Handle deleted nodes — deactivate skills that ONLY cover the deleted node
	if event.ChangeType == "deleted" {
		for _, nodeID := range event.ChangedNodeIDs {
			skills, err := gs.store.GetByGraphNodes([]string{nodeID}, "", 0, 100)
			if err != nil {
				continue
			}
			for _, sk := range skills {
				if len(sk.GraphNodeIDs) == 1 && sk.GraphNodeIDs[0] == nodeID {
					if err := gs.store.DeactivateSkill(sk.ID); err != nil {
						log.Printf("graph_sync: deactivate skill %s: %v", sk.ID, err)
					} else {
						log.Printf("graph_sync: deactivated skill %q (only covered deleted node %s)", sk.Name, nodeID)
					}
				}
			}
		}
	}

	// Handle renamed nodes — update graph_node_ids to include new ID
	// (The rename event carries OldID→NewID in a convention: ChangedNodeIDs[0]=old, ChangedNodeIDs[1]=new)
	if event.ChangeType == "renamed" && len(event.ChangedNodeIDs) == 2 {
		oldID, newID := event.ChangedNodeIDs[0], event.ChangedNodeIDs[1]
		skills, err := gs.store.GetByGraphNodes([]string{oldID}, "", 0, 100)
		if err == nil {
			for _, sk := range skills {
				updated := replaceOrAppend(sk.GraphNodeIDs, oldID, newID)
				_, err := gs.store.pool.Exec(nil,
					`UPDATE skills SET graph_node_ids = $1 WHERE id = $2`, updated, sk.ID)
				if err != nil {
					log.Printf("graph_sync: update node IDs for skill %s: %v", sk.ID, err)
				}
			}
		}
	}

	log.Printf("graph_sync: change type=%s in repo=%s — %d node(s) processed",
		event.ChangeType, event.RepoID, len(event.ChangedNodeIDs))
	return nil
}

// ClearStaleTag removes the "graph_changed" negative tag from a skill.
func (gs *GraphSync) ClearStaleTag(skillID string) error {
	_, err := gs.store.pool.Exec(nil, `
		UPDATE skills
		SET negative_tags = (
			SELECT jsonb_agg(elem)
			FROM jsonb_array_elements(negative_tags) AS elem
			WHERE elem->>'context' != 'graph_changed'
		)
		WHERE id = $1`, skillID)
	return err
}

func replaceOrAppend(ids []string, old, new_ string) []string {
	result := make([]string, 0, len(ids))
	found := false
	for _, id := range ids {
		if id == old {
			result = append(result, new_)
			found = true
		} else {
			result = append(result, id)
		}
	}
	if !found {
		result = append(result, new_)
	}
	return result
}
