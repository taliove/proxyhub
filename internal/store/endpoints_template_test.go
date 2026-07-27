package store

import (
	"errors"
	"testing"
)

// TestEndpointTemplateName tests template_name column read/write in endpoints table.
func TestEndpointTemplateName(t *testing.T) {
	st := newTestStore(t)

	// Create user and template library
	userID := int64(123)
	_, err := st.CreateTemplate(userID, "mobile", "port: 7890\nproxies: {{nodes}}")
	if err != nil {
		t.Fatalf("create template: %v", err)
	}

	// Create endpoint without template_name (empty = follow default)
	ep1, err := st.CreateEndpointForUser(userID, "test-ep-1")
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	if ep1.TemplateName != "" {
		t.Errorf("new endpoint should have empty template_name, got %q", ep1.TemplateName)
	}

	// Update endpoint to use specific template
	err = st.UpdateEndpointTemplate(userID, ep1.ID, "mobile")
	if err != nil {
		t.Fatalf("update template: %v", err)
	}

	// Verify update
	ep1After, err := st.GetEndpointByIDForUser(userID, ep1.ID)
	if err != nil {
		t.Fatalf("get endpoint: %v", err)
	}
	if ep1After.TemplateName != "mobile" {
		t.Errorf("template_name = %q, want mobile", ep1After.TemplateName)
	}

	// Clear template_name (reset to follow default)
	err = st.UpdateEndpointTemplate(userID, ep1.ID, "")
	if err != nil {
		t.Fatalf("clear template: %v", err)
	}
	ep1Cleared, err := st.GetEndpointByIDForUser(userID, ep1.ID)
	if err != nil {
		t.Fatalf("get endpoint after clear: %v", err)
	}
	if ep1Cleared.TemplateName != "" {
		t.Errorf("cleared template_name = %q, want empty", ep1Cleared.TemplateName)
	}
}

// TestUpdateEndpointTemplateValidation tests validation when binding non-existent template.
func TestUpdateEndpointTemplateValidation(t *testing.T) {
	st := newTestStore(t)

	userID := int64(456)
	ep, err := st.CreateEndpointForUser(userID, "test-ep")
	if err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	// Bind non-existent template should fail
	err = st.UpdateEndpointTemplate(userID, ep.ID, "nonexistent")
	if err == nil {
		t.Fatal("binding non-existent template should fail")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

// TestUpdateEndpointTemplateOwnership tests cross-user template binding rejection.
func TestUpdateEndpointTemplateOwnership(t *testing.T) {
	st := newTestStore(t)

	user1 := int64(111)
	user2 := int64(222)

	// User1 creates template
	_, err := st.CreateTemplate(user1, "private", "content")
	if err != nil {
		t.Fatalf("create template for user1: %v", err)
	}

	// User2 creates endpoint
	ep2, err := st.CreateEndpointForUser(user2, "ep2")
	if err != nil {
		t.Fatalf("create endpoint for user2: %v", err)
	}

	// User2 tries to bind user1's template - should fail
	err = st.UpdateEndpointTemplate(user2, ep2.ID, "private")
	if err == nil {
		t.Fatal("cross-user template binding should fail")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}
