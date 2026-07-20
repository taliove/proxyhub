package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// retagAllKind implements batch recomputation of node tags.
// It iterates through the node pool and calls recompute for each node,
// persisting progress after each node for resumability.
type retagAllKind struct {
	nodePool  func() []string
	recompute func(nodeKey string) error
}

// NewRetagAllKind creates a retag_all kind that recomputes tags for all nodes.
// nodePool returns the current list of node keys.
// recompute executes tag recomputation for a single node.
func NewRetagAllKind(store interface {
	AllNodeKeys() ([]string, error)
	RecomputeNodeTags(nodeKey string) error
}) Kind {
	if store == nil {
		return &retagAllKind{
			nodePool:  func() []string { return nil },
			recompute: func(string) error { return nil },
		}
	}
	return &retagAllKind{
		nodePool: func() []string {
			keys, err := store.AllNodeKeys()
			if err != nil {
				return nil
			}
			return keys
		},
		recompute: store.RecomputeNodeTags,
	}
}

func (k *retagAllKind) Name() string {
	return "retag_all"
}

func (k *retagAllKind) Resumable() bool {
	return true
}

// Run executes the batch retagging operation.
// cursor is the number of nodes already processed (empty string = start from beginning).
// Emits progress events for UI display and calls progress() after each node for persistence.
func (k *retagAllKind) Run(
	ctx context.Context,
	params json.RawMessage,
	cursor string,
	emit func(json.RawMessage),
	progress func(cursor string),
) error {
	pool := k.nodePool()
	total := len(pool)

	// Parse cursor to determine starting position
	start := 0
	if cursor != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil {
			return fmt.Errorf("invalid cursor %q: %w", cursor, err)
		}
		start = parsed
	}

	// Emit start event
	startEvent := map[string]interface{}{
		"phase":     "start",
		"total":     total,
		"processed": start,
	}
	if data, err := json.Marshal(startEvent); err == nil {
		emit(data)
	}

	// Process nodes from cursor position
	for i := start; i < total; i++ {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		nodeKey := pool[i]
		if err := k.recompute(nodeKey); err != nil {
			// Continue on error, emit error event
			errEvent := map[string]interface{}{
				"phase":    "error",
				"nodeKey":  nodeKey,
				"error":    err.Error(),
				"progress": i + 1,
				"total":    total,
			}
			if data, err := json.Marshal(errEvent); err == nil {
				emit(data)
			}
		}

		// Update cursor after each node
		newCursor := strconv.Itoa(i + 1)
		progress(newCursor)

		// Emit progress event
		progressEvent := map[string]interface{}{
			"phase":     "progress",
			"nodeKey":   nodeKey,
			"processed": i + 1,
			"total":     total,
		}
		if data, err := json.Marshal(progressEvent); err == nil {
			emit(data)
		}
	}

	// Emit completion event
	doneEvent := map[string]interface{}{
		"phase":     "done",
		"processed": total,
		"total":     total,
	}
	if data, err := json.Marshal(doneEvent); err == nil {
		emit(data)
	}

	return nil
}
