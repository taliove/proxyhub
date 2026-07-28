package server

import (
	"errors"
	"net/http"

	"github.com/taliove/proxyhub/internal/captcha"
)

// captchaService is the login captcha seam. Production wires
// captcha.Service; tests substitute a deterministic stub.
type captchaService interface {
	// Issue renders a fresh challenge for ip, or captcha.ErrRateLimited when
	// the IP exhausted its per-window issue budget.
	Issue(ip string) (captcha.Challenge, error)
	// Verify reports whether answer solves challenge id. Submitting consumes
	// the challenge either way (single use), so a wrong answer forces the
	// client to fetch a fresh one.
	Verify(id, answer string) bool
}

// handleIssueCaptcha serves GET /api/captcha. Unauthenticated by design: the
// login page needs an image before any session exists. Abuse is bounded by
// the per-IP issue throttle inside the captcha service (429 on exhaustion).
func (s *Server) handleIssueCaptcha(w http.ResponseWriter, r *http.Request) {
	if s.captcha == nil {
		http.Error(w, "captcha unavailable", http.StatusServiceUnavailable)
		return
	}
	ip := clientIP(r)
	ch, err := s.captcha.Issue(ip)
	if err != nil {
		if errors.Is(err, captcha.ErrRateLimited) {
			s.logger.Warn("captcha issue rate limited", "ip", ip)
			http.Error(w, "too many captcha requests", http.StatusTooManyRequests)
			return
		}
		s.logger.Error("issue captcha failed", "ip", ip, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, ch)
}

// captchaRequiredForIP reports whether the login attempt from ip must carry a
// captcha answer. Loopback is exempt (same carve-out as ban and honeypot).
// The decision is driven by banned_ips.fail_count for the IP; an absent row
// counts as 0, so threshold 0 means "captcha always on".
func (s *Server) captchaRequiredForIP(ip string, threshold int) bool {
	if s.captcha == nil || isLoopbackIP(ip) {
		return false
	}
	return s.loginFailCount(ip) >= threshold
}

// loginFailCount reads the current failure count for ip. Unknown IP or a
// read error yields 0: a DB hiccup must not lock legitimate users out behind
// a captcha they cannot be told about (the ban path already fails closed).
func (s *Server) loginFailCount(ip string) int {
	rows, err := s.st.ListBannedIPs()
	if err != nil {
		s.logger.Warn("read login fail count failed", "ip", ip, "error", err)
		return 0
	}
	for _, row := range rows {
		if row.IP == ip {
			return row.FailCount
		}
	}
	return 0
}

// loopbackIPv4 is the exact literal the login carve-outs (ban, honeypot,
// captcha) exempt. Deliberately narrower than isLoopbackAddr in
// handlers_trusted_ips.go: see the comment on isLoopbackIP.
const loopbackIPv4 = "127.0.0.1"

// isLoopbackIP mirrors the existing "127.0.0.1 is exempt" convention used by
// the ban and honeypot checks in handleLogin. Literal match by design; the
// broader parse-based check lives in isLoopbackAddr (handlers_trusted_ips.go)
// for the standing-grant decision.
func isLoopbackIP(ip string) bool {
	return ip == loopbackIPv4
}

// recordCaptchaFailure books a missing or wrong captcha answer on the same
// counter as a wrong password, so brute force behind a captcha still walks
// into IP2Ban, and writes the captcha_failure audit event.
func (s *Server) recordCaptchaFailure(ip, username, challengeID, userAgent string, policy securityPolicy) {
	detail := "验证码校验失败"
	if challengeID == "" {
		detail = "缺少验证码"
	}
	s.recordAudit("captcha_failure", ip, username, detail, userAgent)
	s.chargeLoginFailure(ip, username, userAgent, policy, failureReasonCaptcha)
}
