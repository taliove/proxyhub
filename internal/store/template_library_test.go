package store

import (
	"errors"
	"strings"
	"testing"
)

// TestTemplateLibraryMigration verifies the migration is idempotent and existing
// name='clash' rows become default templates.
func TestTemplateLibraryMigration(t *testing.T) {
	s := newTestStore(t)

	// Pre-condition: existing user with old-style template (name='clash')
	userID := int64(1)
	oldContent := "old-style-clash-template"
	if err := s.SetClashTemplateForUser(userID, oldContent); err != nil {
		t.Fatalf("seed old template: %v", err)
	}

	// Apply migration (called by migrate())
	if err := s.migrateTemplateLibrary(); err != nil {
		t.Fatalf("migrateTemplateLibrary: %v", err)
	}

	// Verify: old template is now in library and marked default
	templates, err := s.ListTemplatesForUser(userID)
	if err != nil {
		t.Fatalf("ListTemplatesForUser: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("expected 1 template after migration, got %d", len(templates))
	}
	if templates[0].Name != "clash" {
		t.Errorf("expected name='clash', got %q", templates[0].Name)
	}
	if templates[0].Content != oldContent {
		t.Errorf("content mismatch: expected %q, got %q", oldContent, templates[0].Content)
	}
	if !templates[0].IsDefault {
		t.Error("migrated clash template should be marked default")
	}

	// Verify GetDefaultTemplate returns it
	def, err := s.GetDefaultTemplate(userID)
	if err != nil {
		t.Fatalf("GetDefaultTemplate: %v", err)
	}
	if def.Name != "clash" || def.Content != oldContent {
		t.Errorf("GetDefaultTemplate mismatch: got name=%q content=%q", def.Name, def.Content)
	}

	// Idempotency: run migration again
	if err := s.migrateTemplateLibrary(); err != nil {
		t.Fatalf("second migrateTemplateLibrary: %v", err)
	}
	templates2, _ := s.ListTemplatesForUser(userID)
	if len(templates2) != 1 {
		t.Errorf("migration not idempotent: expected 1 template, got %d", len(templates2))
	}
}

// TestTemplateLibraryMigrationPreservesDefault is a regression test: re-running
// the migration (as happens on every service restart) must not reset a custom
// default template back to is_default=0. The old guard keyed on the transient
// template_library table, which disappears after rename, so every restart
// re-applied the rebuild and recomputed is_default (only name='clash' stayed
// default).
func TestTemplateLibraryMigrationPreservesDefault(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	if _, err := s.CreateTemplate(userID, "custom", "custom-content"); err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if err := s.SetDefaultTemplate(userID, "custom"); err != nil {
		t.Fatalf("SetDefaultTemplate: %v", err)
	}

	// Simulate a service restart: migrate() runs again
	if err := s.migrateTemplateLibrary(); err != nil {
		t.Fatalf("re-run migrateTemplateLibrary: %v", err)
	}

	def, err := s.GetDefaultTemplate(userID)
	if err != nil {
		t.Fatalf("GetDefaultTemplate: %v", err)
	}
	if def.Name != "custom" {
		t.Errorf("default template lost after migration re-run: got %q, want %q", def.Name, "custom")
	}
}

// TestTemplateLibraryCRUD tests create, list, get, update, delete.
func TestTemplateLibraryCRUD(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	// Create first template (auto-default when library empty)
	t1, err := s.CreateTemplate(userID, "template-1", "content-1")
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if t1.Name != "template-1" || t1.Content != "content-1" {
		t.Errorf("t1 mismatch: got %+v", t1)
	}
	if !t1.IsDefault {
		t.Error("first template should auto-default")
	}

	// Create second template (not default)
	t2, err := s.CreateTemplate(userID, "template-2", "content-2")
	if err != nil {
		t.Fatalf("CreateTemplate t2: %v", err)
	}
	if t2.IsDefault {
		t.Error("second template should not auto-default")
	}

	// List
	list, err := s.ListTemplatesForUser(userID)
	if err != nil {
		t.Fatalf("ListTemplatesForUser: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(list))
	}

	// Get by name
	got, err := s.GetTemplateByName(userID, "template-1")
	if err != nil {
		t.Fatalf("GetTemplateByName: %v", err)
	}
	if got.Content != "content-1" {
		t.Errorf("GetTemplateByName content mismatch: got %q", got.Content)
	}

	// Update
	if err := s.UpdateTemplate(userID, "template-1", "updated-content"); err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	updated, _ := s.GetTemplateByName(userID, "template-1")
	if updated.Content != "updated-content" {
		t.Errorf("update not persisted: got %q", updated.Content)
	}

	// Delete non-default
	if err := s.DeleteTemplate(userID, "template-2"); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
	if _, err := s.GetTemplateByName(userID, "template-2"); err != ErrNotFound {
		t.Errorf("expected ErrNotFound after delete, got %v", err)
	}

	// Delete default (allowed, soft reference)
	if err := s.DeleteTemplate(userID, "template-1"); err != nil {
		t.Fatalf("DeleteTemplate default: %v", err)
	}
	listAfter, _ := s.ListTemplatesForUser(userID)
	if len(listAfter) != 0 {
		t.Errorf("expected empty library after deleting all, got %d", len(listAfter))
	}
}

// TestTemplateLibraryUniqueConstraint verifies UNIQUE(user_id, name).
func TestTemplateLibraryUniqueConstraint(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	_, err := s.CreateTemplate(userID, "dupe", "content-1")
	if err != nil {
		t.Fatalf("first create: %v", err)
	}

	// Duplicate name for same user should fail
	_, err = s.CreateTemplate(userID, "dupe", "content-2")
	if err == nil {
		t.Fatal("expected error for duplicate name, got nil")
	}
	if !errors.Is(err, ErrDuplicateName) {
		t.Errorf("expected ErrDuplicateName, got: %v", err)
	}

	// Same name for different user is allowed
	user2 := int64(2)
	_, err = s.CreateTemplate(user2, "dupe", "user2-content")
	if err != nil {
		t.Errorf("same name for different user should succeed, got: %v", err)
	}
}

// TestTemplateLibrarySetDefault tests default marker logic.
func TestTemplateLibrarySetDefault(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	// Create two templates
	s.CreateTemplate(userID, "t1", "c1")
	s.CreateTemplate(userID, "t2", "c2")

	// Set t2 as default
	if err := s.SetDefaultTemplate(userID, "t2"); err != nil {
		t.Fatalf("SetDefaultTemplate: %v", err)
	}

	// Verify only t2 is default
	def, err := s.GetDefaultTemplate(userID)
	if err != nil {
		t.Fatalf("GetDefaultTemplate: %v", err)
	}
	if def.Name != "t2" {
		t.Errorf("expected default=t2, got %q", def.Name)
	}

	list, _ := s.ListTemplatesForUser(userID)
	var defaultCount int
	for _, tmpl := range list {
		if tmpl.IsDefault {
			defaultCount++
		}
	}
	if defaultCount != 1 {
		t.Errorf("expected exactly 1 default template, got %d", defaultCount)
	}
}

// TestTemplateLibraryGetDefault tests fallback chain.
func TestTemplateLibraryGetDefault(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	// No user template: should fall back to global default
	def, err := s.GetDefaultTemplate(userID)
	if err != nil {
		t.Fatalf("GetDefaultTemplate with no user template: %v", err)
	}
	if def == nil {
		t.Fatal("expected fallback to global default, got nil")
	}
	// Should return global template content (from system_settings or embedded)

	// Create user template
	s.CreateTemplate(userID, "user-default", "user-content")
	s.SetDefaultTemplate(userID, "user-default")

	defUser, err := s.GetDefaultTemplate(userID)
	if err != nil {
		t.Fatalf("GetDefaultTemplate with user template: %v", err)
	}
	if defUser.Name != "user-default" || defUser.Content != "user-content" {
		t.Errorf("expected user template, got name=%q content=%q", defUser.Name, defUser.Content)
	}
}

// TestTemplateLibraryUserIsolation verifies users cannot see each other's templates.
func TestTemplateLibraryUserIsolation(t *testing.T) {
	s := newTestStore(t)
	user1, user2 := int64(1), int64(2)

	s.CreateTemplate(user1, "user1-template", "u1-content")
	s.CreateTemplate(user2, "user2-template", "u2-content")

	// User1 cannot see user2's template
	_, err := s.GetTemplateByName(user1, "user2-template")
	if err != ErrNotFound {
		t.Errorf("user1 should not see user2 template, got error: %v", err)
	}

	// User2 cannot see user1's template
	list2, _ := s.ListTemplatesForUser(user2)
	if len(list2) != 1 || list2[0].Name != "user2-template" {
		t.Errorf("user2 list isolation failed: got %+v", list2)
	}

	// User2 cannot delete user1's template (should return not found)
	err = s.DeleteTemplate(user2, "user1-template")
	if err != ErrNotFound {
		t.Errorf("delete other user's template should return ErrNotFound, got: %v", err)
	}
	// Verify user1 template still exists
	u1t, _ := s.GetTemplateByName(user1, "user1-template")
	if u1t == nil {
		t.Error("user1 template was incorrectly deleted by user2")
	}
}

// TestTemplateQuotaEnforcement tests max_templates quota.
func TestTemplateQuotaEnforcement(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	// Set quota to 2 templates
	if err := s.addColumnIfMissing("user_quotas", "max_templates", "INTEGER NOT NULL DEFAULT 10"); err != nil {
		t.Fatalf("add max_templates column: %v", err)
	}
	quota := &UserQuota{
		UserID:       userID,
		MaxAirports:  5,
		MaxEndpoints: 5,
	}
	s.UpsertUserQuota(quota)

	// Update quota with max_templates=2
	_, err := s.db.Exec(`UPDATE user_quotas SET max_templates = 2 WHERE user_id = ?`, userID)
	if err != nil {
		t.Fatalf("set max_templates: %v", err)
	}

	// Create 2 templates (should succeed)
	_, err = s.CreateTemplate(userID, "t1", "c1")
	if err != nil {
		t.Fatalf("create t1: %v", err)
	}
	_, err = s.CreateTemplate(userID, "t2", "c2")
	if err != nil {
		t.Fatalf("create t2: %v", err)
	}

	// Third template should fail quota check
	_, err = s.CreateTemplate(userID, "t3", "c3")
	if err == nil {
		t.Fatal("expected quota exceeded error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "quota") {
		t.Errorf("expected quota error, got: %v", err)
	}
}

// TestCountEndpointsUsingTemplate tests reference counting for soft delete.
func TestCountEndpointsUsingTemplate(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	// Ensure endpoints.template_name column exists
	if err := s.addColumnIfMissing("endpoints", "template_name", "TEXT NOT NULL DEFAULT ''"); err != nil {
		t.Fatalf("add template_name column: %v", err)
	}

	// Create template
	s.CreateTemplate(userID, "my-template", "content")

	// Create 2 endpoints referencing it
	ep1, _ := s.CreateEndpointForUser(userID, "ep1")
	ep2, _ := s.CreateEndpointForUser(userID, "ep2")
	_, err := s.db.Exec(`UPDATE endpoints SET template_name = ? WHERE id IN (?, ?)`,
		"my-template", ep1.ID, ep2.ID)
	if err != nil {
		t.Fatalf("update endpoints: %v", err)
	}

	// Count references
	count, err := s.CountEndpointsUsingTemplate(userID, "my-template")
	if err != nil {
		t.Fatalf("CountEndpointsUsingTemplate: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 references, got %d", count)
	}

	// Delete template (soft reference: should succeed)
	if err := s.DeleteTemplate(userID, "my-template"); err != nil {
		t.Fatalf("DeleteTemplate with references: %v", err)
	}

	// Endpoints still exist (soft reference)
	gotEp1, _ := s.GetEndpointByID(ep1.ID)
	if gotEp1 == nil {
		t.Error("endpoint should still exist after template deletion")
	}
}
