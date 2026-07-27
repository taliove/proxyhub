package store

import (
	"errors"
	"testing"
)

// TestTemplateVersionsMigration verifies the template_versions table migration
// is idempotent and creates the required schema.
func TestTemplateVersionsMigration(t *testing.T) {
	s := newTestStore(t)

	// Migration already applied during newTestStore -> Open -> migrate
	// Verify table exists by inserting a test version
	userID := int64(1)
	tmpl, err := s.CreateTemplate(userID, "test", "content-v1")
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	// Query versions (should have 1 version from creation)
	versions, err := s.ListVersions(userID, "test")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version after create, got %d", len(versions))
	}
	if versions[0].Version != 1 {
		t.Errorf("expected version=1, got %d", versions[0].Version)
	}

	// Re-run migration (idempotency test)
	if err := s.migrateTemplateVersions(); err != nil {
		t.Fatalf("second migrateTemplateVersions: %v", err)
	}

	// Versions should still be intact
	versions2, _ := s.ListVersions(userID, "test")
	if len(versions2) != 1 {
		t.Errorf("migration not idempotent: expected 1 version, got %d", len(versions2))
	}

	// Verify template still accessible
	tmpl2, err := s.GetTemplateByName(userID, "test")
	if err != nil || tmpl2.ID != tmpl.ID {
		t.Errorf("template corrupted after migration re-run")
	}
}

// TestCreateTemplateAppendsVersion verifies CreateTemplate appends version 1.
func TestCreateTemplateAppendsVersion(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	_, err := s.CreateTemplate(userID, "new-template", "initial-content")
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}

	// Should have exactly 1 version
	versions, err := s.ListVersions(userID, "new-template")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}
	if versions[0].Version != 1 {
		t.Errorf("expected version 1, got %d", versions[0].Version)
	}

	// Verify content matches
	vc, err := s.GetVersionContent(userID, "new-template", 1)
	if err != nil {
		t.Fatalf("GetVersionContent: %v", err)
	}
	if vc.Content != "initial-content" {
		t.Errorf("version content mismatch: got %q", vc.Content)
	}
}

// TestUpdateTemplateAppendsVersion verifies UpdateTemplate increments version.
func TestUpdateTemplateAppendsVersion(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	s.CreateTemplate(userID, "template", "v1-content")

	// First update -> version 2
	if err := s.UpdateTemplate(userID, "template", "v2-content"); err != nil {
		t.Fatalf("UpdateTemplate v2: %v", err)
	}

	versions, _ := s.ListVersions(userID, "template")
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions after update, got %d", len(versions))
	}
	// Should be descending order (newest first)
	if versions[0].Version != 2 || versions[1].Version != 1 {
		t.Errorf("expected versions [2, 1], got [%d, %d]", versions[0].Version, versions[1].Version)
	}

	// Second update -> version 3
	s.UpdateTemplate(userID, "template", "v3-content")
	versions2, _ := s.ListVersions(userID, "template")
	if len(versions2) != 3 {
		t.Fatalf("expected 3 versions, got %d", len(versions2))
	}
	if versions2[0].Version != 3 {
		t.Errorf("expected newest version=3, got %d", versions2[0].Version)
	}

	// Verify each version content
	v1, _ := s.GetVersionContent(userID, "template", 1)
	v2, _ := s.GetVersionContent(userID, "template", 2)
	v3, _ := s.GetVersionContent(userID, "template", 3)
	if v1.Content != "v1-content" || v2.Content != "v2-content" || v3.Content != "v3-content" {
		t.Errorf("version content mismatch: v1=%q v2=%q v3=%q", v1.Content, v2.Content, v3.Content)
	}
}

// TestVersionTrimTo20 verifies old versions are pruned beyond 20.
func TestVersionTrimTo20(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	s.CreateTemplate(userID, "template", "v1")

	// Create 22 more versions (total 23)
	for i := 2; i <= 23; i++ {
		if err := s.UpdateTemplate(userID, "template", "content"); err != nil {
			t.Fatalf("UpdateTemplate v%d: %v", i, err)
		}
	}

	versions, err := s.ListVersions(userID, "template")
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}

	// Should only keep 20 most recent (versions 4-23)
	if len(versions) != 20 {
		t.Fatalf("expected 20 versions after trim, got %d", len(versions))
	}

	// Oldest should be version 4, newest version 23
	if versions[19].Version != 4 {
		t.Errorf("expected oldest version=4, got %d", versions[19].Version)
	}
	if versions[0].Version != 23 {
		t.Errorf("expected newest version=23, got %d", versions[0].Version)
	}

	// Verify old versions are deleted
	_, err = s.GetVersionContent(userID, "template", 1)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("version 1 should be deleted, got error: %v", err)
	}
	_, err = s.GetVersionContent(userID, "template", 3)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("version 3 should be deleted, got error: %v", err)
	}

	// Version 4 should still exist
	v4, err := s.GetVersionContent(userID, "template", 4)
	if err != nil {
		t.Errorf("version 4 should exist, got error: %v", err)
	}
	if v4 == nil {
		t.Error("version 4 should not be nil")
	}
}

// TestDeleteTemplateCascadesVersions verifies versions are deleted with template.
func TestDeleteTemplateCascadesVersions(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	s.CreateTemplate(userID, "template", "v1")
	s.UpdateTemplate(userID, "template", "v2")
	s.UpdateTemplate(userID, "template", "v3")

	// Verify 3 versions exist
	versions, _ := s.ListVersions(userID, "template")
	if len(versions) != 3 {
		t.Fatalf("expected 3 versions before delete, got %d", len(versions))
	}

	// Delete template
	if err := s.DeleteTemplate(userID, "template"); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}

	// Versions should be gone
	versions2, err := s.ListVersions(userID, "template")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("ListVersions after delete should return ErrNotFound, got: %v", err)
	}
	if len(versions2) != 0 {
		t.Errorf("expected 0 versions after delete, got %d", len(versions2))
	}

	// GetVersionContent should also return not found
	_, err = s.GetVersionContent(userID, "template", 1)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetVersionContent after delete should return ErrNotFound, got: %v", err)
	}
}

// TestVersionTransactionAtomicity verifies versions are only persisted if
// template operation succeeds (transaction atomicity).
func TestVersionTransactionAtomicity(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	s.CreateTemplate(userID, "template", "v1")

	// Attempt update with invalid empty content (should fail validation)
	err := s.UpdateTemplate(userID, "template", "")
	if err == nil {
		t.Fatal("UpdateTemplate with empty content should fail")
	}

	// Version should NOT have been appended
	versions, _ := s.ListVersions(userID, "template")
	if len(versions) != 1 {
		t.Errorf("failed update should not append version, got %d versions", len(versions))
	}

	// Attempt create with duplicate name (should fail)
	_, err = s.CreateTemplate(userID, "template", "duplicate")
	if !errors.Is(err, ErrDuplicateName) {
		t.Fatalf("expected ErrDuplicateName, got: %v", err)
	}

	// No new template or version should exist
	versions2, _ := s.ListVersions(userID, "template")
	if len(versions2) != 1 {
		t.Errorf("failed create should not create versions, got %d", len(versions2))
	}
}

// TestVersionUserIsolation verifies users cannot access each other's versions.
func TestVersionUserIsolation(t *testing.T) {
	s := newTestStore(t)
	user1, user2 := int64(1), int64(2)

	// User1 creates template
	s.CreateTemplate(user1, "template", "user1-v1")
	s.UpdateTemplate(user1, "template", "user1-v2")

	// User2 creates template with same name
	s.CreateTemplate(user2, "template", "user2-v1")

	// User1 should see 2 versions
	v1, _ := s.ListVersions(user1, "template")
	if len(v1) != 2 {
		t.Errorf("user1 should see 2 versions, got %d", len(v1))
	}

	// User2 should see 1 version
	v2, _ := s.ListVersions(user2, "template")
	if len(v2) != 1 {
		t.Errorf("user2 should see 1 version, got %d", len(v2))
	}

	// User2 cannot access user1's versions
	_, err := s.GetVersionContent(user2, "template", 2)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("user2 should not access user1 version 2, got: %v", err)
	}

	// User1 can access their own version 2
	vc, err := s.GetVersionContent(user1, "template", 2)
	if err != nil || vc.Content != "user1-v2" {
		t.Errorf("user1 should access their version 2")
	}
}

// TestVersionCrossUserTemplateNotFound verifies accessing non-existent
// template returns ErrNotFound.
func TestVersionCrossUserTemplateNotFound(t *testing.T) {
	s := newTestStore(t)
	userID := int64(1)

	// No templates exist
	_, err := s.ListVersions(userID, "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("ListVersions for nonexistent template should return ErrNotFound, got: %v", err)
	}

	_, err = s.GetVersionContent(userID, "nonexistent", 1)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("GetVersionContent for nonexistent template should return ErrNotFound, got: %v", err)
	}
}
