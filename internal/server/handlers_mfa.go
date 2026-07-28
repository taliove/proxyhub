package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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

// Second login stage (login hardening ticket 06).
//
// handleLoginMFA serves POST /api/login/mfa. It is unauthenticated because no
// session exists yet: the credential is the mfa_pending token handed out by the
// password stage, which is bound to one user, one source address, a 5 minute
// TTL and a budget of mfaPendingMaxFailures wrong codes.
//
// Request: {mfa_pending_token, code, trust_ip?}. The code is tried as a TOTP
// first and as a recovery code second, so a user who fat-fingers a TOTP does
// not silently burn a recovery code. Every failure charges both the pending
// budget (which caps grinding against one handoff) and the per-IP login failure
// counter (which walks the address into IP2Ban at the same threshold as wrong
// passwords), so an attacker cannot buy unlimited attempts by re-running the
// password stage.
func (s *Server) handleLoginMFA(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)

	var req struct {
		PendingToken string `json:"mfa_pending_token"`
		Code         string `json:"code"`
		TrustIP      bool   `json:"trust_ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Resolve the handoff before looking at the code. Peek rather than Consume:
	// a wrong code must spend budget, not destroy a legitimate handoff.
	// Expired, unknown, foreign-IP and budget-exhausted tokens are all one
	// undifferentiated 401 - telling them apart would map out the state for an
	// attacker holding a stolen token.
	pending, ok := s.mfaPending.Peek(req.PendingToken, ip)
	if !ok {
		http.Error(w, "invalid or expired verification session", http.StatusUnauthorized)
		return
	}
	if strings.TrimSpace(req.Code) == "" {
		// Not an attempt: no budget charged, no counter moved.
		writeJSONStatus(w, http.StatusBadRequest, map[string]any{
			"error": "verification code is required",
		})
		return
	}

	user, err := s.st.GetUserByID(pending.UserID)
	if err != nil {
		s.mfaPending.Destroy(req.PendingToken)
		if errors.Is(err, store.ErrNotFound) {
			// The account disappeared between the two stages.
			http.Error(w, "invalid or expired verification session", http.StatusUnauthorized)
			return
		}
		s.logger.Error("load user for mfa login failed", "user_id", pending.UserID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Re-check the account state: it may have been disabled while the handoff
	// was open, and the password stage's verdict must not outlive that.
	if user.Disabled() {
		s.mfaPending.Destroy(req.PendingToken)
		s.recordAudit("login_disabled", ip, user.Username, "account disabled during mfa challenge")
		http.Error(w, "account disabled", http.StatusForbidden)
		return
	}

	cfg, err := s.st.GetUserMFAConfig(pending.UserID)
	if err != nil {
		s.logger.Error("load mfa config for login failed", "user_id", pending.UserID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !cfg.Enabled {
		// MFA was reset (admin or CLI) while the handoff was open: there is
		// nothing left to verify, so the handoff is void and the user restarts.
		s.mfaPending.Destroy(req.PendingToken)
		http.Error(w, "invalid or expired verification session", http.StatusUnauthorized)
		return
	}

	method, ok := s.verifyLoginSecondFactor(cfg, req.Code)
	if !ok {
		s.recordMFALoginFailure(req.PendingToken, ip, user.Username)
		http.Error(w, "invalid verification code", http.StatusUnauthorized)
		return
	}

	// Redeem the handoff. One-shot: concurrent submissions of the same token
	// yield exactly one session, and a replay after success is a plain 401.
	if _, ok := s.mfaPending.Consume(req.PendingToken, ip); !ok {
		http.Error(w, "invalid or expired verification session", http.StatusUnauthorized)
		return
	}

	if req.TrustIP {
		if err := s.st.AddTrustedIP(user.ID, ip); err != nil {
			// The login itself succeeded; losing the convenience grant only
			// costs the user another challenge next time.
			s.logger.Warn("trust login ip failed", "user_id", user.ID, "ip", ip, "error", err)
		} else {
			s.recordAudit("trusted_ip_added", ip, user.Username,
				fmt.Sprintf("trusted for %d days after mfa login", int(store.TrustedIPTTL.Hours()/24)))
		}
	}

	// "mfa=totp" / "mfa=recovery" is the marker
	// store.GetTrustRecommendationCount counts (detail LIKE '%mfa=%').
	s.issueLoginSession(w, user, ip, "mfa="+method)
}

// verifyLoginSecondFactor checks code against the account's TOTP secret first
// and its recovery codes second, returning which factor matched ("totp" or
// "recovery"). A matching recovery code is consumed here: unlike the
// regeneration path, nothing replaces the batch afterwards, so a code that let
// someone in must never let anyone in twice.
func (s *Server) verifyLoginSecondFactor(cfg *store.UserMFAConfig, code string) (method string, ok bool) {
	if mfa.VerifyTOTP(cfg.TOTPSecret, code) {
		return "totp", true
	}

	encoded, err := encodeStoredRecoveryHashes(cfg.RecoveryCodesHash)
	if err != nil {
		s.logger.Error("encode recovery hashes failed", "user_id", cfg.UserID, "error", err)
		return "", false
	}
	remaining, matched, err := mfa.VerifyRecoveryCode(encoded, code)
	if err != nil {
		s.logger.Error("verify recovery code failed", "user_id", cfg.UserID, "error", err)
		return "", false
	}
	if !matched {
		return "", false
	}

	hashes, err := decodeStoredRecoveryHashes(remaining)
	if err != nil {
		s.logger.Error("decode remaining recovery hashes failed", "user_id", cfg.UserID, "error", err)
		return "", false
	}
	if err := s.st.UpdateUser(cfg.UserID, store.UserUpdate{RecoveryCodesHash: &hashes}); err != nil {
		// Refuse the login rather than let a code stay usable: a recovery code
		// that cannot be burned is a replayable credential.
		s.logger.Error("burn recovery code failed", "user_id", cfg.UserID, "error", err)
		return "", false
	}
	return "recovery", true
}

// recordMFALoginFailure books one wrong second factor: it charges the pending
// budget (destroying the handoff on exhaustion), charges the per-IP login
// failure counter on the same threshold as a wrong password, and writes the
// mfa_failure audit row. The detail carries only the first 8 characters of the
// pending token so failures can be correlated without persisting a live
// credential.
func (s *Server) recordMFALoginFailure(pendingToken, ip, username string) {
	alive := s.mfaPending.RecordFailure(pendingToken)

	policy := s.loadSecurityPolicy()
	now := time.Now()
	nowBanned, err := s.st.RecordLoginFailure(ip, policy.BanThreshold, policy.BanDuration, now)
	if err != nil {
		s.logger.Error("record mfa failure failed", "ip", ip, "error", err)
	}

	detail := fmt.Sprintf("mfa verification failed, pending=%s", pendingTokenPrefix(pendingToken))
	if !alive {
		detail += ", pending session destroyed"
	}
	s.recordAudit("mfa_failure", ip, username, detail)

	if nowBanned {
		s.logger.Warn("ip banned after repeated mfa failures", "ip", ip)
		s.recordAudit("threshold_ban", ip, username,
			fmt.Sprintf("连续失败达阈值 %d，封禁至 %s",
				policy.BanThreshold, now.Add(policy.BanDuration).Format("2006-01-02 15:04:05")))
	}
}

// pendingTokenPrefix renders the audit-safe fragment of a pending token.
func pendingTokenPrefix(token string) string {
	const prefixLen = 8
	if len(token) <= prefixLen {
		return token
	}
	return token[:prefixLen]
}
