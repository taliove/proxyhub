package store

import (
	"testing"
)

func TestCreateDistributionNode(t *testing.T) {
	st := newTestStore(t)

	node := &DistributionNode{
		Name:             "US Distribution",
		Region:           "US",
		DistributionPath: "/us-relay",
		UpstreamNodeKeys: []string{"server1.com:443", "server2.com:443"},
		LBStrategy:       "random",
		Enabled:          true,
	}

	if err := st.CreateDistributionNode(node); err != nil {
		t.Fatalf("CreateDistributionNode() error = %v", err)
	}

	if node.ID == 0 {
		t.Error("CreateDistributionNode() did not set ID")
	}

	// Verify node was created
	retrieved, err := st.GetDistributionNode(node.ID)
	if err != nil {
		t.Fatalf("GetDistributionNode() error = %v", err)
	}

	if retrieved.Name != "US Distribution" {
		t.Errorf("Name = %q, want %q", retrieved.Name, "US Distribution")
	}
	if retrieved.Region != "US" {
		t.Errorf("Region = %q, want %q", retrieved.Region, "US")
	}
	if retrieved.DistributionPath != "/us-relay" {
		t.Errorf("DistributionPath = %q, want %q", retrieved.DistributionPath, "/us-relay")
	}
	if len(retrieved.UpstreamNodeKeys) != 2 {
		t.Errorf("len(UpstreamNodeKeys) = %d, want 2", len(retrieved.UpstreamNodeKeys))
	}
	if retrieved.LBStrategy != "random" {
		t.Errorf("LBStrategy = %q, want %q", retrieved.LBStrategy, "random")
	}
	if !retrieved.Enabled {
		t.Error("Enabled = false, want true")
	}
}

func TestCreateDistributionNode_Validation(t *testing.T) {
	st := newTestStore(t)

	tests := []struct {
		name    string
		node    *DistributionNode
		wantErr bool
	}{
		{
			name: "missing name",
			node: &DistributionNode{
				DistributionPath: "/path",
			},
			wantErr: true,
		},
		{
			name: "missing distribution_path",
			node: &DistributionNode{
				Name: "Test",
			},
			wantErr: true,
		},
		{
			name: "valid minimal",
			node: &DistributionNode{
				Name:             "Valid",
				DistributionPath: "/valid",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := st.CreateDistributionNode(tt.node)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateDistributionNode() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCreateDistributionNode_UniqueDistributionPath(t *testing.T) {
	st := newTestStore(t)

	node1 := &DistributionNode{
		Name:             "Node 1",
		DistributionPath: "/unique-path",
	}
	if err := st.CreateDistributionNode(node1); err != nil {
		t.Fatalf("CreateDistributionNode() error = %v", err)
	}

	// Try to create another node with same distribution_path
	node2 := &DistributionNode{
		Name:             "Node 2",
		DistributionPath: "/unique-path",
	}
	err := st.CreateDistributionNode(node2)
	if err == nil {
		t.Error("CreateDistributionNode() with duplicate distribution_path should fail")
	}
}

func TestUpdateDistributionNode(t *testing.T) {
	st := newTestStore(t)

	node := &DistributionNode{
		Name:             "Original",
		Region:           "US",
		DistributionPath: "/original",
		UpstreamNodeKeys: []string{"node1"},
		LBStrategy:       "random",
		Enabled:          true,
	}
	if err := st.CreateDistributionNode(node); err != nil {
		t.Fatalf("CreateDistributionNode() error = %v", err)
	}

	// Update the node
	node.Name = "Updated"
	node.Region = "EU"
	node.DistributionPath = "/updated"
	node.UpstreamNodeKeys = []string{"node1", "node2", "node3"}
	node.LBStrategy = "round_robin"
	node.Enabled = false

	if err := st.UpdateDistributionNode(node); err != nil {
		t.Fatalf("UpdateDistributionNode() error = %v", err)
	}

	// Verify updates
	retrieved, err := st.GetDistributionNode(node.ID)
	if err != nil {
		t.Fatalf("GetDistributionNode() error = %v", err)
	}

	if retrieved.Name != "Updated" {
		t.Errorf("Name = %q, want %q", retrieved.Name, "Updated")
	}
	if retrieved.Region != "EU" {
		t.Errorf("Region = %q, want %q", retrieved.Region, "EU")
	}
	if retrieved.DistributionPath != "/updated" {
		t.Errorf("DistributionPath = %q, want %q", retrieved.DistributionPath, "/updated")
	}
	if len(retrieved.UpstreamNodeKeys) != 3 {
		t.Errorf("len(UpstreamNodeKeys) = %d, want 3", len(retrieved.UpstreamNodeKeys))
	}
	if retrieved.LBStrategy != "round_robin" {
		t.Errorf("LBStrategy = %q, want %q", retrieved.LBStrategy, "round_robin")
	}
	if retrieved.Enabled {
		t.Error("Enabled = true, want false")
	}
}

func TestUpdateDistributionNode_NonExistent(t *testing.T) {
	st := newTestStore(t)

	node := &DistributionNode{
		ID:               999,
		Name:             "NonExistent",
		DistributionPath: "/nonexistent",
	}

	err := st.UpdateDistributionNode(node)
	if err == nil {
		t.Error("UpdateDistributionNode() with non-existent ID should fail")
	}
}

func TestDeleteDistributionNode(t *testing.T) {
	st := newTestStore(t)

	node := &DistributionNode{
		Name:             "To Delete",
		DistributionPath: "/delete-me",
	}
	if err := st.CreateDistributionNode(node); err != nil {
		t.Fatalf("CreateDistributionNode() error = %v", err)
	}

	// Delete the node
	if err := st.DeleteDistributionNode(node.ID); err != nil {
		t.Fatalf("DeleteDistributionNode() error = %v", err)
	}

	// Verify it's gone
	_, err := st.GetDistributionNode(node.ID)
	if err == nil {
		t.Error("GetDistributionNode() should fail after delete")
	}
}

func TestDeleteDistributionNode_NonExistent(t *testing.T) {
	st := newTestStore(t)

	err := st.DeleteDistributionNode(999)
	if err == nil {
		t.Error("DeleteDistributionNode() with non-existent ID should fail")
	}
}

func TestSetDistributionNodeEnabled(t *testing.T) {
	st := newTestStore(t)

	node := &DistributionNode{
		Name:             "Toggle Test",
		DistributionPath: "/toggle",
		Enabled:          true,
	}
	if err := st.CreateDistributionNode(node); err != nil {
		t.Fatalf("CreateDistributionNode() error = %v", err)
	}

	// Disable the node
	if err := st.SetDistributionNodeEnabled(node.ID, false); err != nil {
		t.Fatalf("SetDistributionNodeEnabled(false) error = %v", err)
	}

	retrieved, err := st.GetDistributionNode(node.ID)
	if err != nil {
		t.Fatalf("GetDistributionNode() error = %v", err)
	}
	if retrieved.Enabled {
		t.Error("Enabled = true, want false after disabling")
	}

	// Enable the node
	if err := st.SetDistributionNodeEnabled(node.ID, true); err != nil {
		t.Fatalf("SetDistributionNodeEnabled(true) error = %v", err)
	}

	retrieved, err = st.GetDistributionNode(node.ID)
	if err != nil {
		t.Fatalf("GetDistributionNode() error = %v", err)
	}
	if !retrieved.Enabled {
		t.Error("Enabled = false, want true after enabling")
	}
}

func TestListDistributionNodes(t *testing.T) {
	st := newTestStore(t)

	// Create enabled nodes
	for i := 1; i <= 3; i++ {
		node := &DistributionNode{
			Name:             "Enabled Node",
			DistributionPath: "/enabled-" + string(rune('0'+i)),
			Enabled:          true,
		}
		if err := st.CreateDistributionNode(node); err != nil {
			t.Fatalf("CreateDistributionNode() error = %v", err)
		}
	}

	// Create disabled nodes
	for i := 1; i <= 2; i++ {
		node := &DistributionNode{
			Name:             "Disabled Node",
			DistributionPath: "/disabled-" + string(rune('0'+i)),
			Enabled:          false,
		}
		if err := st.CreateDistributionNode(node); err != nil {
			t.Fatalf("CreateDistributionNode() error = %v", err)
		}
	}

	// ListDistributionNodes should only return enabled nodes
	nodes, err := st.ListDistributionNodes()
	if err != nil {
		t.Fatalf("ListDistributionNodes() error = %v", err)
	}

	if len(nodes) != 3 {
		t.Errorf("len(ListDistributionNodes()) = %d, want 3", len(nodes))
	}

	for _, node := range nodes {
		if !node.Enabled {
			t.Errorf("ListDistributionNodes() returned disabled node: %+v", node)
		}
	}
}

func TestListAllDistributionNodes(t *testing.T) {
	st := newTestStore(t)

	// Create enabled nodes
	for i := 1; i <= 3; i++ {
		node := &DistributionNode{
			Name:             "Enabled Node",
			DistributionPath: "/all-enabled-" + string(rune('0'+i)),
			Enabled:          true,
		}
		if err := st.CreateDistributionNode(node); err != nil {
			t.Fatalf("CreateDistributionNode() error = %v", err)
		}
	}

	// Create disabled nodes
	for i := 1; i <= 2; i++ {
		node := &DistributionNode{
			Name:             "Disabled Node",
			DistributionPath: "/all-disabled-" + string(rune('0'+i)),
			Enabled:          false,
		}
		if err := st.CreateDistributionNode(node); err != nil {
			t.Fatalf("CreateDistributionNode() error = %v", err)
		}
	}

	// ListAllDistributionNodes should return all nodes
	nodes, err := st.ListAllDistributionNodes()
	if err != nil {
		t.Fatalf("ListAllDistributionNodes() error = %v", err)
	}

	if len(nodes) != 5 {
		t.Errorf("len(ListAllDistributionNodes()) = %d, want 5", len(nodes))
	}

	enabledCount := 0
	disabledCount := 0
	for _, node := range nodes {
		if node.Enabled {
			enabledCount++
		} else {
			disabledCount++
		}
	}

	if enabledCount != 3 {
		t.Errorf("enabled count = %d, want 3", enabledCount)
	}
	if disabledCount != 2 {
		t.Errorf("disabled count = %d, want 2", disabledCount)
	}
}

func TestDistributionNode_EmptyUpstreamNodeKeys(t *testing.T) {
	st := newTestStore(t)

	node := &DistributionNode{
		Name:             "Empty Upstream",
		DistributionPath: "/empty",
		UpstreamNodeKeys: []string{},
		Enabled:          true,
	}

	if err := st.CreateDistributionNode(node); err != nil {
		t.Fatalf("CreateDistributionNode() error = %v", err)
	}

	retrieved, err := st.GetDistributionNode(node.ID)
	if err != nil {
		t.Fatalf("GetDistributionNode() error = %v", err)
	}

	if retrieved.UpstreamNodeKeys == nil {
		t.Error("UpstreamNodeKeys should not be nil")
	}
	if len(retrieved.UpstreamNodeKeys) != 0 {
		t.Errorf("len(UpstreamNodeKeys) = %d, want 0", len(retrieved.UpstreamNodeKeys))
	}
}

func TestDistributionNode_NilUpstreamNodeKeys(t *testing.T) {
	st := newTestStore(t)

	node := &DistributionNode{
		Name:             "Nil Upstream",
		DistributionPath: "/nil",
		UpstreamNodeKeys: nil,
		Enabled:          true,
	}

	if err := st.CreateDistributionNode(node); err != nil {
		t.Fatalf("CreateDistributionNode() error = %v", err)
	}

	retrieved, err := st.GetDistributionNode(node.ID)
	if err != nil {
		t.Fatalf("GetDistributionNode() error = %v", err)
	}

	// nil should be marshaled as empty JSON array and unmarshaled as empty slice
	if retrieved.UpstreamNodeKeys == nil {
		t.Error("UpstreamNodeKeys should not be nil after round-trip")
	}
}
