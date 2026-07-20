package server

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// SessionManager 内存会话管理
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]time.Time // token → 过期时间
	ttl      time.Duration
}

// NewSessionManager 创建会话管理器
func NewSessionManager(ttl time.Duration) *SessionManager {
	return &SessionManager{
		sessions: make(map[string]time.Time),
		ttl:      ttl,
	}
}

// Create 创建新会话，返回会话 Token
func (m *SessionManager) Create() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked()
	m.sessions[token] = time.Now().Add(m.ttl)
	return token, nil
}

// Validate 验证会话是否有效
func (m *SessionManager) Validate(token string) bool {
	if token == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	expiry, ok := m.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(m.sessions, token)
		return false
	}
	return true
}

// Destroy 销毁会话
func (m *SessionManager) Destroy(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, token)
}

// cleanupLocked 清理过期会话（调用方需持有锁）
func (m *SessionManager) cleanupLocked() {
	now := time.Now()
	for token, expiry := range m.sessions {
		if now.After(expiry) {
			delete(m.sessions, token)
		}
	}
}
