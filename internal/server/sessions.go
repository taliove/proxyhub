package server

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// SessionPayload is the identity attached to a session token. The zero
// value (Legacy=true) represents a pre-ticket-02 session created against
// the legacy settings-KV single admin; such sessions resolve their user id
// lazily to the first super_admin row at the handler layer.
type SessionPayload struct {
	UserID             int64
	Role               string
	MustChangePassword bool
	// ActingUserID is the super admin's impersonation target (ticket 07):
	// set via POST /api/admin/switch-user, cleared via exit-switch.
	ActingUserID int64
	// Legacy marks sessions created without user identity (settings-KV
	// era). They keep working until ticket 02 retires the legacy login.
	Legacy bool

	expiry time.Time
}

// Expired reports whether the session has passed its TTL.
func (p SessionPayload) Expired(now time.Time) bool { return now.After(p.expiry) }

// SessionManager 内存会话管理
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]SessionPayload // token → payload
	ttl      time.Duration
}

// NewSessionManager 创建会话管理器
func NewSessionManager(ttl time.Duration) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]SessionPayload),
		ttl:      ttl,
	}
}

// Create 创建新会话，返回会话 Token(legacy 无身份载荷,兼容 ticket 02 之前的登录路径)
func (m *SessionManager) Create() (string, error) {
	return m.CreateWithPayload(SessionPayload{Legacy: true})
}

// CreateWithPayload 创建携带身份载荷的会话(ticket 02 登录路径用)
func (m *SessionManager) CreateWithPayload(p SessionPayload) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	p.expiry = time.Now().Add(m.ttl)
	m.sessions[token] = p
	return token, nil
}

// Validate 验证会话是否有效
func (m *SessionManager) Validate(token string) bool {
	_, ok := m.Lookup(token)
	return ok
}

// Lookup 取回会话载荷;不存在或已过期返回 ok=false(过期顺带清除)
func (m *SessionManager) Lookup(token string) (SessionPayload, bool) {
	if token == "" {
		return SessionPayload{}, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	p, ok := m.sessions[token]
	if !ok {
		return SessionPayload{}, false
	}
	if p.Expired(time.Now()) {
		delete(m.sessions, token)
		return SessionPayload{}, false
	}
	return p, true
}

// SetActingUser 更新会话的超管视角目标(ticket 07):target<=0 表示回到自身视角。
// 会话不存在或已过期返回 false。
func (m *SessionManager) SetActingUser(token string, target int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.sessions[token]
	if !ok || p.Expired(time.Now()) {
		return false
	}
	p.ActingUserID = target
	m.sessions[token] = p
	return true
}

// Destroy 销毁会话
func (m *SessionManager) Destroy(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

// DestroyForUser 吊销某用户的全部会话。禁用/删除/重置密码/改密时调用:
// 凭证或状态变更后,旧会话一律不得继续放行。
func (m *SessionManager) DestroyForUser(userID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for token, p := range m.sessions {
		if p.UserID == userID {
			delete(m.sessions, token)
		}
	}
}

// cleanupLocked 清理过期会话（调用方需持有锁）
func (m *SessionManager) cleanupLocked() {
	now := time.Now()
	for token, p := range m.sessions {
		if p.Expired(now) {
			delete(m.sessions, token)
		}
	}
}
