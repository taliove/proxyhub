package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrQuotaExceeded is returned when a per-user resource quota is exceeded.
var ErrQuotaExceeded = errors.New("quota exceeded")

// ErrDuplicateName is returned when a template name already exists in the user's library.
var ErrDuplicateName = errors.New("duplicate name")

// Template represents a user's configuration template in the library.
type Template struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"user_id"`
	Name      string    `json:"name"`
	Content   string    `json:"content"`
	IsDefault bool      `json:"is_default"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TemplateVersion represents a historical version of a template.
type TemplateVersion struct {
	ID         int64     `json:"id"`
	TemplateID int64     `json:"template_id"`
	Version    int       `json:"version"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

// migrateTemplateVersions creates the template_versions table if it doesn't exist.
// Called by migrate() after template table is ready. Idempotent.
func (s *Store) migrateTemplateVersions() error {
	// Guard: check if template_versions table exists
	if _, err := s.db.Exec(`SELECT version FROM template_versions LIMIT 0`); err == nil {
		return nil
	}

	// Create table in single transaction (idempotent via IF NOT EXISTS)
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS template_versions (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			template_id INTEGER NOT NULL,
			version     INTEGER NOT NULL,
			content     TEXT NOT NULL,
			created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(template_id, version)
		)
	`)
	if err != nil {
		return fmt.Errorf("create template_versions: %w", err)
	}

	// Create index for efficient lookups
	if _, err := s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_template_versions_template_id ON template_versions(template_id)`); err != nil {
		return fmt.Errorf("create template_versions index: %w", err)
	}

	return nil
}

// migrateTemplateLibrary applies the template library schema upgrade (ticket 01).
// Migrates existing template table to template_library with is_default column and
// UNIQUE(user_id, name) constraint. Marks existing name='clash' rows as default.
// Idempotent: safe to call multiple times.
func (s *Store) migrateTemplateLibrary() error {
	// Already applied when the template table carries the is_default column.
	// Keying the guard on the transient template_library table is wrong: that
	// table is renamed to template at the end, so every restart would re-run
	// the rebuild and recompute is_default (wiping any non-'clash' default).
	if _, err := s.db.Exec(`SELECT is_default FROM template LIMIT 0`); err == nil {
		return nil
	}

	// Run the rebuild atomically so a crash cannot strand a half-migrated state.
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin template library migration: %w", err)
	}
	defer tx.Rollback()

	// Create new table with UNIQUE constraint and is_default column
	if _, err := tx.Exec(`
		CREATE TABLE template_library (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id    INTEGER NOT NULL DEFAULT 0,
			name       TEXT NOT NULL DEFAULT '',
			content    TEXT NOT NULL DEFAULT '',
			is_default INTEGER NOT NULL DEFAULT 0,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(user_id, name)
		)
	`); err != nil {
		return fmt.Errorf("create template_library: %w", err)
	}

	// Migrate existing rows, marking name='clash' as default
	if _, err := tx.Exec(`
		INSERT OR IGNORE INTO template_library (id, user_id, name, content, is_default, created_at, updated_at)
		SELECT id, user_id, name, content,
			   CASE WHEN name = 'clash' THEN 1 ELSE 0 END as is_default,
			   created_at, updated_at
		FROM template
	`); err != nil {
		return fmt.Errorf("migrate template rows: %w", err)
	}

	// Drop old table and rename new one
	if _, err := tx.Exec(`DROP TABLE template`); err != nil {
		return fmt.Errorf("drop old template table: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE template_library RENAME TO template`); err != nil {
		return fmt.Errorf("rename template_library: %w", err)
	}

	// Create index on user_id (multiTenantTables already has this but ensure it exists)
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_template_user_id ON template(user_id)`); err != nil {
		return fmt.Errorf("create template user_id index: %w", err)
	}

	return tx.Commit()
}

// ListTemplatesForUser returns all templates for a user, ordered by name.
func (s *Store) ListTemplatesForUser(userID int64) ([]*Template, error) {
	rows, err := s.db.Query(`
		SELECT id, user_id, name, content, is_default, created_at, updated_at
		FROM template
		WHERE user_id = ?
		ORDER BY name
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("query templates: %w", err)
	}
	defer rows.Close()

	var templates []*Template
	for rows.Next() {
		t := &Template{}
		var isDefault int
		if err := rows.Scan(&t.ID, &t.UserID, &t.Name, &t.Content, &isDefault, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan template: %w", err)
		}
		t.IsDefault = isDefault == 1
		templates = append(templates, t)
	}
	return templates, rows.Err()
}

// CreateTemplate creates a new template in the user's library.
// If the library is empty, the new template becomes default automatically.
// Enforces max_templates quota if set (ErrQuotaExceeded when full,
// ErrDuplicateName when (user_id, name) already exists).
// Appends version 1 and trims to 20 most recent versions in the same transaction.
func (s *Store) CreateTemplate(userID int64, name, content string) (*Template, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: template name is required", ErrInvalidInput)
	}
	if content == "" {
		return nil, fmt.Errorf("%w: template content is required", ErrInvalidInput)
	}

	// Check quota
	var maxTemplates int
	err := s.db.QueryRow(`SELECT max_templates FROM user_quotas WHERE user_id = ?`, userID).Scan(&maxTemplates)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check quota: %w", err)
	}
	// Default quota is 10 if no row exists
	if errors.Is(err, sql.ErrNoRows) {
		maxTemplates = 10
	}

	// Count existing templates
	var count int
	err = s.db.QueryRow(`SELECT COUNT(*) FROM template WHERE user_id = ?`, userID).Scan(&count)
	if err != nil {
		return nil, fmt.Errorf("count templates: %w", err)
	}
	if count >= maxTemplates {
		return nil, fmt.Errorf("%w: template quota exceeded (max %d)", ErrQuotaExceeded, maxTemplates)
	}

	// Check if library is empty (auto-default for first template)
	isFirst := count == 0

	// Begin transaction for atomic template creation + version append
	tx, err := s.db.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	res, err := tx.Exec(`
		INSERT INTO template (user_id, name, content, is_default, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, userID, name, content, boolToInt(isFirst))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, fmt.Errorf("template %q: %w", name, ErrDuplicateName)
		}
		return nil, fmt.Errorf("insert template: %w", err)
	}

	templateID, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get template id: %w", err)
	}

	// Append version 1
	if err := s.appendVersionTx(tx, templateID, content); err != nil {
		return nil, fmt.Errorf("append version: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit transaction: %w", err)
	}

	return s.getTemplateByID(templateID)
}

// GetTemplateByName returns a template by name for the user.
func (s *Store) GetTemplateByName(userID int64, name string) (*Template, error) {
	t := &Template{}
	var isDefault int
	err := s.db.QueryRow(`
		SELECT id, user_id, name, content, is_default, created_at, updated_at
		FROM template
		WHERE user_id = ? AND name = ?
	`, userID, name).Scan(&t.ID, &t.UserID, &t.Name, &t.Content, &isDefault, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get template by name: %w", err)
	}
	t.IsDefault = isDefault == 1
	return t, nil
}

// getTemplateByID returns a template by ID (internal helper).
func (s *Store) getTemplateByID(id int64) (*Template, error) {
	t := &Template{}
	var isDefault int
	err := s.db.QueryRow(`
		SELECT id, user_id, name, content, is_default, created_at, updated_at
		FROM template
		WHERE id = ?
	`, id).Scan(&t.ID, &t.UserID, &t.Name, &t.Content, &isDefault, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get template by id: %w", err)
	}
	t.IsDefault = isDefault == 1
	return t, nil
}

// UpdateTemplate updates the content of an existing template.
// Appends a new version and trims to 20 most recent versions in the same transaction.
func (s *Store) UpdateTemplate(userID int64, name, content string) error {
	if content == "" {
		return fmt.Errorf("%w: template content is required", ErrInvalidInput)
	}

	// Begin transaction for atomic update + version append
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get template ID
	var templateID int64
	err = tx.QueryRow(`SELECT id FROM template WHERE user_id = ? AND name = ?`, userID, name).Scan(&templateID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get template id: %w", err)
	}

	// Update template content
	res, err := tx.Exec(`
		UPDATE template
		SET content = ?, updated_at = CURRENT_TIMESTAMP
		WHERE user_id = ? AND name = ?
	`, content, userID, name)
	if err != nil {
		return fmt.Errorf("update template: %w", err)
	}
	if err := checkAffected(res); err != nil {
		return err
	}

	// Append version
	if err := s.appendVersionTx(tx, templateID, content); err != nil {
		return fmt.Errorf("append version: %w", err)
	}

	return tx.Commit()
}

// DeleteTemplate deletes a template from the user's library.
// Soft reference: allows deletion even if endpoints reference it.
// Cascades deletion to all template versions in the same transaction.
func (s *Store) DeleteTemplate(userID int64, name string) error {
	// Begin transaction for atomic template + versions deletion
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Get template ID
	var templateID int64
	err = tx.QueryRow(`SELECT id FROM template WHERE user_id = ? AND name = ?`, userID, name).Scan(&templateID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("get template id: %w", err)
	}

	// Delete all versions for this template
	if _, err := tx.Exec(`DELETE FROM template_versions WHERE template_id = ?`, templateID); err != nil {
		return fmt.Errorf("delete versions: %w", err)
	}

	// Delete template
	res, err := tx.Exec(`DELETE FROM template WHERE user_id = ? AND name = ?`, userID, name)
	if err != nil {
		return fmt.Errorf("delete template: %w", err)
	}
	if err := checkAffected(res); err != nil {
		return err
	}

	return tx.Commit()
}

// SetDefaultTemplate marks a template as the user's default (clears other defaults).
func (s *Store) SetDefaultTemplate(userID int64, name string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear all defaults for this user
	if _, err := tx.Exec(`UPDATE template SET is_default = 0 WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("clear defaults: %w", err)
	}

	// Set the new default
	res, err := tx.Exec(`UPDATE template SET is_default = 1 WHERE user_id = ? AND name = ?`, userID, name)
	if err != nil {
		return fmt.Errorf("set default: %w", err)
	}
	if err := checkAffected(res); err != nil {
		return err
	}

	return tx.Commit()
}

// GetDefaultTemplate returns the user's default template, or falls back to
// global default (system_settings.clash_template) then embedded default.
// Returns a synthesized Template struct for fallback cases.
func (s *Store) GetDefaultTemplate(userID int64) (*Template, error) {
	// Try user's default template
	var t Template
	var isDefault int
	err := s.db.QueryRow(`
		SELECT id, user_id, name, content, is_default, created_at, updated_at
		FROM template
		WHERE user_id = ? AND is_default = 1
		LIMIT 1
	`, userID).Scan(&t.ID, &t.UserID, &t.Name, &t.Content, &isDefault, &t.CreatedAt, &t.UpdatedAt)
	if err == nil {
		t.IsDefault = true
		return &t, nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("query default template: %w", err)
	}

	// Fallback: global default (existing GetClashTemplate behavior)
	content, err := s.GetClashTemplate()
	if err != nil {
		return nil, fmt.Errorf("get global default: %w", err)
	}

	// Return synthesized template (not in user's library)
	return &Template{
		ID:        0, // Sentinel for "global fallback"
		UserID:    0,
		Name:      "global-default",
		Content:   content,
		IsDefault: true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}, nil
}

// CountEndpointsUsingTemplate counts how many endpoints reference this template.
// Used for soft-delete warning UI.
func (s *Store) CountEndpointsUsingTemplate(userID int64, templateName string) (int, error) {
	var count int
	err := s.db.QueryRow(`
		SELECT COUNT(*)
		FROM endpoints
		WHERE user_id = ? AND template_name = ?
	`, userID, templateName).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count endpoints using template: %w", err)
	}
	return count, nil
}

// appendVersionTx appends a new version for a template and trims to 20 most recent.
// Must be called within a transaction. Version number is auto-incremented.
func (s *Store) appendVersionTx(tx *sql.Tx, templateID int64, content string) error {
	// Get next version number
	var maxVersion sql.NullInt64
	err := tx.QueryRow(`SELECT MAX(version) FROM template_versions WHERE template_id = ?`, templateID).Scan(&maxVersion)
	if err != nil {
		return fmt.Errorf("get max version: %w", err)
	}

	nextVersion := 1
	if maxVersion.Valid {
		nextVersion = int(maxVersion.Int64) + 1
	}

	// Insert new version
	_, err = tx.Exec(`
		INSERT INTO template_versions (template_id, version, content)
		VALUES (?, ?, ?)
	`, templateID, nextVersion, content)
	if err != nil {
		return fmt.Errorf("insert version: %w", err)
	}

	// Trim to 20 most recent versions
	_, err = tx.Exec(`
		DELETE FROM template_versions
		WHERE template_id = ?
		AND version < (
			SELECT version FROM template_versions
			WHERE template_id = ?
			ORDER BY version DESC
			LIMIT 1 OFFSET 19
		)
	`, templateID, templateID)
	if err != nil {
		return fmt.Errorf("trim versions: %w", err)
	}

	return nil
}

// ListVersions returns version metadata (version number and created_at) for a template,
// ordered by version descending (newest first). Returns ErrNotFound if template
// doesn't exist or user doesn't own it.
func (s *Store) ListVersions(userID int64, name string) ([]TemplateVersion, error) {
	// Get template ID and verify ownership
	var templateID int64
	err := s.db.QueryRow(`SELECT id FROM template WHERE user_id = ? AND name = ?`, userID, name).Scan(&templateID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}

	rows, err := s.db.Query(`
		SELECT id, template_id, version, created_at
		FROM template_versions
		WHERE template_id = ?
		ORDER BY version DESC
	`, templateID)
	if err != nil {
		return nil, fmt.Errorf("query versions: %w", err)
	}
	defer rows.Close()

	var versions []TemplateVersion
	for rows.Next() {
		var v TemplateVersion
		if err := rows.Scan(&v.ID, &v.TemplateID, &v.Version, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		versions = append(versions, v)
	}
	return versions, rows.Err()
}

// GetVersionContent returns the full content of a specific version.
// Returns ErrNotFound if template doesn't exist, user doesn't own it, or version doesn't exist.
func (s *Store) GetVersionContent(userID int64, name string, version int) (*TemplateVersion, error) {
	// Get template ID and verify ownership
	var templateID int64
	err := s.db.QueryRow(`SELECT id FROM template WHERE user_id = ? AND name = ?`, userID, name).Scan(&templateID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get template: %w", err)
	}

	// Get version content
	var v TemplateVersion
	err = s.db.QueryRow(`
		SELECT id, template_id, version, content, created_at
		FROM template_versions
		WHERE template_id = ? AND version = ?
	`, templateID, version).Scan(&v.ID, &v.TemplateID, &v.Version, &v.Content, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get version: %w", err)
	}

	return &v, nil
}

