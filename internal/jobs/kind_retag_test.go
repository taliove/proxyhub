package jobs

import (
	"context"
	"encoding/json"
	"testing"
)

// TestRetagAllKind_Registration tests that retag_all kind registers correctly.
func TestRetagAllKind_Registration(t *testing.T) {
	k := NewRetagAllKind(nil)
	if k.Name() != "retag_all" {
		t.Errorf("expected name 'retag_all', got %q", k.Name())
	}
	if !k.Resumable() {
		t.Error("expected retag_all to be resumable")
	}
}

// TestRetagAllKind_EmptyPool tests behavior when node pool is empty.
func TestRetagAllKind_EmptyPool(t *testing.T) {
	var poolCalls int
	nodePool := func() []string { poolCalls++; return nil }
	var recomputeCalls int
	recompute := func(key string) error { recomputeCalls++; return nil }

	k := &retagAllKind{nodePool: nodePool, recompute: recompute}
	ctx := context.Background()
	emit := func(json.RawMessage) {}
	progress := func(string) {}

	err := k.Run(ctx, nil, "", emit, progress)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if poolCalls != 1 {
		t.Errorf("expected 1 nodePool call, got %d", poolCalls)
	}
	if recomputeCalls != 0 {
		t.Errorf("expected 0 recompute calls for empty pool, got %d", recomputeCalls)
	}
}

// TestRetagAllKind_ProcessesAllNodes tests full pool processing.
func TestRetagAllKind_ProcessesAllNodes(t *testing.T) {
	pool := []string{"node1", "node2", "node3"}
	nodePool := func() []string { return pool }
	var processed []string
	recompute := func(key string) error {
		processed = append(processed, key)
		return nil
	}

	k := &retagAllKind{nodePool: nodePool, recompute: recompute}
	ctx := context.Background()
	emit := func(json.RawMessage) {}
	progress := func(string) {}

	err := k.Run(ctx, nil, "", emit, progress)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(processed) != 3 {
		t.Errorf("expected 3 processed nodes, got %d", len(processed))
	}
	for i, expected := range pool {
		if i >= len(processed) || processed[i] != expected {
			t.Errorf("node[%d]: expected %q, got %q", i, expected, processed[i])
		}
	}
}

// TestRetagAllKind_ResumesFromCursor tests cursor-based resumption.
func TestRetagAllKind_ResumesFromCursor(t *testing.T) {
	pool := []string{"node1", "node2", "node3", "node4"}
	nodePool := func() []string { return pool }
	var processed []string
	recompute := func(key string) error {
		processed = append(processed, key)
		return nil
	}

	k := &retagAllKind{nodePool: nodePool, recompute: recompute}
	ctx := context.Background()
	emit := func(json.RawMessage) {}
	progress := func(string) {}

	// Resume from cursor "2" (already processed 2 nodes)
	err := k.Run(ctx, nil, "2", emit, progress)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// Should process node3 and node4 only
	expected := []string{"node3", "node4"}
	if len(processed) != len(expected) {
		t.Fatalf("expected %d processed nodes, got %d", len(expected), len(processed))
	}
	for i, exp := range expected {
		if processed[i] != exp {
			t.Errorf("node[%d]: expected %q, got %q", i, exp, processed[i])
		}
	}
}

// TestRetagAllKind_EmitsProgressEvents tests progress event emission.
func TestRetagAllKind_EmitsProgressEvents(t *testing.T) {
	pool := []string{"node1", "node2"}
	nodePool := func() []string { return pool }
	recompute := func(key string) error { return nil }

	k := &retagAllKind{nodePool: nodePool, recompute: recompute}
	ctx := context.Background()
	var events []json.RawMessage
	emit := func(data json.RawMessage) { events = append(events, data) }
	progress := func(string) {}

	err := k.Run(ctx, nil, "", emit, progress)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// Expect at least start + 2 progress + done events
	if len(events) < 4 {
		t.Errorf("expected at least 4 events, got %d", len(events))
	}
}

// TestRetagAllKind_CallsProgressCallback tests cursor persistence callback.
func TestRetagAllKind_CallsProgressCallback(t *testing.T) {
	pool := []string{"node1", "node2", "node3"}
	nodePool := func() []string { return pool }
	recompute := func(key string) error { return nil }

	k := &retagAllKind{nodePool: nodePool, recompute: recompute}
	ctx := context.Background()
	emit := func(json.RawMessage) {}
	var cursors []string
	progress := func(cursor string) { cursors = append(cursors, cursor) }

	err := k.Run(ctx, nil, "", emit, progress)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	// Should call progress after each node: "1", "2", "3"
	expected := []string{"1", "2", "3"}
	if len(cursors) != len(expected) {
		t.Fatalf("expected %d progress calls, got %d", len(expected), len(cursors))
	}
	for i, exp := range expected {
		if cursors[i] != exp {
			t.Errorf("cursor[%d]: expected %q, got %q", i, exp, cursors[i])
		}
	}
}

// TestRetagAllKind_Cancellation tests context cancellation.
func TestRetagAllKind_Cancellation(t *testing.T) {
	pool := []string{"node1", "node2", "node3"}
	nodePool := func() []string { return pool }
	var processed int
	recompute := func(key string) error {
		processed++
		return nil
	}

	k := &retagAllKind{nodePool: nodePool, recompute: recompute}
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel after first node
	var callCount int
	emit := func(json.RawMessage) {
		callCount++
		if callCount == 2 { // After start event and first progress
			cancel()
		}
	}
	progress := func(string) {}

	err := k.Run(ctx, nil, "", emit, progress)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	// Should stop early
	if processed >= len(pool) {
		t.Errorf("expected early stop, but processed all %d nodes", processed)
	}
}
