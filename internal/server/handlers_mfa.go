package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/taliove/proxyhub/internal/mfa"
	"github.com/taliove/proxyhub/internal/store"
)

// Multi-factor authentication management surface (login hardening ticket 05).
//
// Enrollment is deliberately two-staged. The first request provisions a secret
// and persists it with totp_enabled=0, which is inert: nothing in the login
// path consults a secret whose enabled flag is clear. Only a request carrying
// a code that verifies against that staged secret flips the flag and issues
// recovery codes. A user who scans the QR into the wrong app, or abandons the
// page, therefore never ends up with an account demanding codes they cannot
// produce.
//
// Recovery codes are returned exactly once, at the moment of confirmation.
// Only their SHA-256 digests are persisted, so a later request - including one
// from a super admin - cannot recover the plaintext. Losing them means
// regenerating (below) or an admin reset.

// handleMFAEnroll serves POST /api/me/mfa/enroll, both stages.
//
// Stage one (no totp_code): provisions and stages a secret, responding with
// {secret, otpauth_url}. Repeat calls re-provision, which is what a user
// reloading the enrollment page needs; the previous staged secret is inert and
// simply discarded.
//
// Stage two (totp_code present): verifies the code against the staged secret
// and only then sets totp_enabled=1 and issues recovery codes, responding with
// {recovery_codes}. A wrong code is a 400 and changes nothing.
//
// Enrollment always acts on the logged-in account, never on an impersonated
// one: a super admin viewing another user must not be able to bind an
// authenticator into that account (that direction is reset-mfa).
func (s *Server) handleMFAEnroll(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}

	// An empty body is a valid stage-one request, so a decode failure is only
	// fatal when the body is present but malformed.
	var req struct {
		TOTPCode string `json:"totp_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	user, err := s.st.GetUserByID(scope.UserID)
	if err != nil {
		s.writeMFAUserError(w, scope.UserID, err)
		return
	}
	cfg, err := s.st.GetUserMFAConfig(scope.UserID)
	if err != nil {
		s.writeMFAUserError(w, scope.UserID, err)
		return
	}

	// Already bound: refuse both stages. Rebinding an authenticator is a
	// credential rotation that must not ride on the session alone; it goes
	// through an admin reset, which is audited.
	if cfg.Enabled {
		writeJSONStatus(w, http.StatusConflict, map[string]any{
			"error": "mfa already enrolled",
		})
		return
	}

	if req.TOTPCode == "" {
		s.stageTOTPSecret(w, user)
		return
	}
	s.confirmTOTPSecret(w, r, user, cfg, req.TOTPCode)
}

// stageTOTPSecret provisions a fresh secret and persists it unenabled.
func (s *Server) stageTOTPSecret(w http.ResponseWriter, user *store.User) {
	key, err := mfa.GenerateTOTPSecret(user.Username)
	if err != nil {
		s.logger.Error("generate totp secret failed", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Stage the secret only. TOTPEnabled stays untouched (still 0), so this
	// write cannot activate MFA on its own.
	enabled := false
	if err := s.st.UpdateUser(user.ID, store.UserUpdate{
		TOTPSecret:  &key.Secret,
		TOTPEnabled: &enabled,
	}); err != nil {
		s.logger.Error("stage totp secret failed", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, map[string]any{
		"secret":       key.Secret,
		"otpauth_url":  key.OTPAuthURL,
		"digits":       6,
		"period":       int(mfa.TOTPPeriod.Seconds()),
		"issuer":       mfa.Issuer,
		"account_name": user.Username,
	})
}

// confirmTOTPSecret verifies code against the staged secret and, on success,
// enables MFA and issues the one-time recovery codes.
func (s *Server) confirmTOTPSecret(w http.ResponseWriter, r *http.Request, user *store.User, cfg *store.UserMFAConfig, code string) {
	if cfg.TOTPSecret == "" {
		// Confirming without a staged secret means the user skipped stage one
		// (or the secret was reset underneath them). Send them back to it.
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"error": "no pending enrollment, request a secret first",
		})
		return
	}
	if !mfa.VerifyTOTP(cfg.TOTPSecret, code) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"error": "invalid verification code",
		})
		return
	}

	plaintext, stored, err := mfa.GenerateRecoveryCodes()
	if err != nil {
		s.logger.Error("generate recovery codes failed", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	hashes, err := decodeStoredRecoveryHashes(stored)
	if err != nil {
		s.logger.Error("decode recovery hashes failed", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	enabled := true
	if err := s.st.UpdateUser(user.ID, store.UserUpdate{
		TOTPEnabled:       &enabled,
		RecoveryCodesHash: &hashes,
	}); err != nil {
		s.logger.Error("enable mfa failed", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.recordAudit("mfa_enrolled", clientIP(r), user.Username,
		fmt.Sprintf("totp enabled, %d recovery codes issued", len(plaintext)))

	// The plaintext codes exist here and nowhere else, ever again.
	writeJSON(w, map[string]any{
		"ok":             true,
		"recovery_codes": plaintext,
	})
}

// handleMFARegenerateRecovery serves POST /api/me/mfa/regenerate-recovery.
//
// Body {code} is a mandatory second-factor confirmation - either a current
// TOTP or an unused recovery code. Without it, a hijacked session could mint
// itself a fresh set of long-lived credentials and, worse, silently invalidate
// the codes the real owner is holding. On success the previous batch is
// replaced wholesale: regeneration is also the "my codes leaked" remedy, so
// partial retention would defeat the purpose.
func (s *Server) handleMFARegenerateRecovery(w http.ResponseWriter, r *http.Request) {
	scope, ok := s.mustUserScope(w, r)
	if !ok {
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"error": "confirmation code is required",
		})
		return
	}

	user, err := s.st.GetUserByID(scope.UserID)
	if err != nil {
		s.writeMFAUserError(w, scope.UserID, err)
		return
	}
	cfg, err := s.st.GetUserMFAConfig(scope.UserID)
	if err != nil {
		s.writeMFAUserError(w, scope.UserID, err)
		return
	}
	// Never-enrolled accounts get their first batch from enrollment only.
	if !cfg.Enabled {
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"error":           "mfa is not enrolled",
			"must_enroll_mfa": true,
		})
		return
	}

	if !s.verifySecondFactor(cfg, req.Code) {
		s.recordAudit("mfa_failure", clientIP(r), user.Username,
			"recovery code regeneration confirmation failed")
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"error": "invalid confirmation code",
		})
		return
	}

	plaintext, stored, err := mfa.GenerateRecoveryCodes()
	if err != nil {
		s.logger.Error("generate recovery codes failed", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	hashes, err := decodeStoredRecoveryHashes(stored)
	if err != nil {
		s.logger.Error("decode recovery hashes failed", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.st.UpdateUser(user.ID, store.UserUpdate{RecoveryCodesHash: &hashes}); err != nil {
		s.logger.Error("regenerate recovery codes failed", "user_id", user.ID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.recordAudit("mfa_recovery_regenerated", clientIP(r), user.Username,
		fmt.Sprintf("%d recovery codes issued, previous batch invalidated", len(plaintext)))
	writeJSON(w, map[string]any{
		"ok":             true,
		"recovery_codes": plaintext,
	})
}

// verifySecondFactor accepts either a current TOTP or an unused recovery code
// as proof of possession. A recovery code used this way is NOT consumed: the
// whole batch is about to be replaced anyway, and consuming it first would
// leave a window where a failed write loses the code for nothing.
func (s *Server) verifySecondFactor(cfg *store.UserMFAConfig, code string) bool {
	if mfa.VerifyTOTP(cfg.TOTPSecret, code) {
		return true
	}
	encoded, err := encodeStoredRecoveryHashes(cfg.RecoveryCodesHash)
	if err != nil {
		s.logger.Error("encode recovery hashes failed", "user_id", cfg.UserID, "error", err)
		return false
	}
	_, ok, err := mfa.VerifyRecoveryCode(encoded, code)
	if err != nil {
		s.logger.Error("verify recovery code failed", "user_id", cfg.UserID, "error", err)
		return false
	}
	return ok
}

// handleAdminResetMFA serves POST /api/admin/users/{id}/reset-mfa (super admin
// only, via adminGuard). This is the operator escape hatch for a user who lost
// both the authenticator and the recovery codes: the target returns to the
// never-enrolled state and is forced through enrollment on next login by
// requireMFAEnrolled.
//
// Sessions are deliberately NOT revoked. The MFA gate re-reads the users table
// on every request, so a live session is re-gated immediately anyway, and
// keeping it lets the user enroll again without a second login round trip.
func (s *Server) handleAdminResetMFA(w http.ResponseWriter, r *http.Request) {
	id, err := parseAdminUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	user, err := s.st.GetUserByID(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("get user failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if err := s.st.ResetUserMFA(id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("reset user mfa failed", "user_id", id, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.recordAudit("mfa_reset", clientIP(r), user.Username,
		fmt.Sprintf("mfa reset for user id=%d by super admin", id))
	writeJSON(w, map[string]any{
		"ok":       true,
		"user_id":  id,
		"username": user.Username,
	})
}

// writeMFAUserError maps a users-table read failure to a response. A missing
// user means the session outlived the account: 401, same as requireAuth.
func (s *Server) writeMFAUserError(w http.ResponseWriter, userID int64, err error) {
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.logger.Error("load user for mfa failed", "user_id", userID, "error", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// decodeStoredRecoveryHashes converts the mfa package's stored representation
// (a JSON array) into the []string the store layer writes. The two layers
// deliberately do not share a type: mfa owns the hashing format, store owns
// the column encoding.
func decodeStoredRecoveryHashes(stored string) ([]string, error) {
	var hashes []string
	if err := json.Unmarshal([]byte(stored), &hashes); err != nil {
		return nil, fmt.Errorf("decode stored recovery hashes: %w", err)
	}
	return hashes, nil
}

// encodeStoredRecoveryHashes is the inverse: it renders the hashes read from
// the store back into the JSON form mfa.VerifyRecoveryCode expects.
func encodeStoredRecoveryHashes(hashes []string) (string, error) {
	if hashes == nil {
		hashes = []string{}
	}
	buf, err := json.Marshal(hashes)
	if err != nil {
		return "", fmt.Errorf("encode stored recovery hashes: %w", err)
	}
	return string(buf), nil
}
