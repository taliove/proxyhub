package server

import (
	"strconv"
	"strings"
	"time"
)

// honeypotUsernames 高敏感用户名。任何针对这些用户名的登录尝试都视为攻击，
// 立即封禁来源 IP —— 合法用户在初始化时被禁止使用这些名字。
var honeypotUsernames = map[string]bool{
	"admin":         true,
	"administrator": true,
	"root":          true,
	"superuser":     true,
	"sysadmin":      true,
	"system":        true,
	"manager":       true,
	"test":          true,
	"guest":         true,
	"operator":      true,
	"webmaster":     true,
}

// isHoneypotUsername 判断用户名是否为高敏感蜜罐账号
func isHoneypotUsername(username string) bool {
	return honeypotUsernames[strings.ToLower(strings.TrimSpace(username))]
}

// securityPolicy 从数据库设置解析出的 IP2Ban 策略
type securityPolicy struct {
	BanThreshold int
	BanDuration  time.Duration
}

const (
	defaultBanThreshold = 5
	defaultBanDuration  = time.Hour
)

// loadSecurityPolicy 从设置读取 IP2Ban 策略，缺省时用默认值
func (s *Server) loadSecurityPolicy() securityPolicy {
	policy := securityPolicy{
		BanThreshold: defaultBanThreshold,
		BanDuration:  defaultBanDuration,
	}

	settings, err := s.st.GetSystemSettings()
	if err != nil {
		return policy
	}

	if v := settings["ban_threshold"]; v != "" {
		if n, err := parsePositiveInt(v); err == nil {
			policy.BanThreshold = n
		}
	}
	if v := settings["ban_duration"]; v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			policy.BanDuration = d
		}
	}
	return policy
}

// parsePositiveInt 解析正整数
func parsePositiveInt(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	if n <= 0 {
		return 0, strconv.ErrRange
	}
	return n, nil
}
