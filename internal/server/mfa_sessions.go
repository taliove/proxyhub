package server

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const (
	// mfaPendingTTL bounds how long the second login stage stays open.
	mfaPendingTTL = 5 * time.Minute
	// mfaPendingMaxFailures is the number of wrong TOTP/recovery codes a
	// single pending session tolerates before it is destroyed, so one
	// password-passed handoff cannot be used to brute-force the code.
	mfaPendingMaxFailures = 5
)

// MFAPendingSession is the state carried between the password stage and the
// second-factor stage of login. It is intentionally memory-only: a 5 minute
// window makes losing it on restart cheap.
type MFAPendingSession struct {
	UserID int64
	// IP is the client address the token was issued to; a submission from
	// any other address is refused.
	IP string

	expiry   time.Time
	failures int
}

// Expired reports whether the pending session has passed its TTL.
func (p MFAPendingSession) Expired(now time.Time) bool { return now.After(p.expiry) }

// MFAPendingManager stores mfa_pending tokens. All operations are safe for
// concurrent use, and consumption is one-shot: exactly one caller can redeem
// a given token.
type MFAPendingManager struct {
	mu      sync.Mutex
	pending map[string]MFAPendingSession
}

// NewMFAPendingManager creates an empty pending-session store.
func NewMFAPendingManager() *MFAPendingManager {
	return &MFAPendingManager{pending: make(map[string]MFAPendingSession)}
}

// Create issues a pending token for userID bound to ip. Creation sweeps
// expired entries, which keeps the map bounded without a background timer.
func (m *MFAPendingManager) Create(userID int64, ip string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	m.pending[token] = MFAPendingSession{
		UserID: userID,
		IP:     ip,
		expiry: time.Now().Add(mfaPendingTTL),
	}
	return token, nil
}

// Consume redeems token for ip. It succeeds at most once per token: on
// success the entry is destroyed before returning. Expiry drops the entry;
// an IP mismatch leaves it in place so the legitimate client on the original
// address can still complete login.
func (m *MFAPendingManager) Consume(token, ip string) (MFAPendingSession, bool) {
	return m.consumeAt(token, ip, time.Now())
}

// consumeAt is Consume with an explicit clock (tests, and any caller that
// needs a fixed reference time).
func (m *MFAPendingManager) consumeAt(token, ip string, now time.Time) (MFAPendingSession, bool) {
	if token == "" {
		return MFAPendingSession{}, false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.pending[token]
	if !ok {
		return MFAPendingSession{}, false
	}
	if p.Expired(now) {
		delete(m.pending, token)
		return MFAPendingSession{}, false
	}
	if p.IP != ip {
		return MFAPendingSession{}, false
	}

	delete(m.pending, token)
	return p, true
}

// RecordFailure charges one wrong second-factor attempt against token and
// reports whether the pending session is still usable afterwards. Reaching
// mfaPendingMaxFailures destroys it; an unknown or expired token reports
// false.
func (m *MFAPendingManager) RecordFailure(token string) (alive bool) {
	if token == "" {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.pending[token]
	if !ok {
		return false
	}
	if p.Expired(time.Now()) {
		delete(m.pending, token)
		return false
	}

	p.failures++
	if p.failures >= mfaPendingMaxFailures {
		delete(m.pending, token)
		return false
	}
	m.pending[token] = p
	return true
}

// Destroy removes a pending session unconditionally (logout, password stage
// restarted, or any handler that decides the handoff is void).
func (m *MFAPendingManager) Destroy(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pending, token)
}

// Len reports the number of live entries, sweeping expired ones first. Used
// by tests and diagnostics.
func (m *MFAPendingManager) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(time.Now())
	return len(m.pending)
}

// cleanupLocked drops expired entries. Callers must hold the mutex.
func (m *MFAPendingManager) cleanupLocked(now time.Time) {
	for token, p := range m.pending {
		if p.Expired(now) {
			delete(m.pending, token)
		}
	}
}
