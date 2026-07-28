package server

import (
	"fmt"
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

// securityPolicy 从数据库设置解析出的 IP2Ban 与验证码策略
type securityPolicy struct {
	BanThreshold int
	BanDuration  time.Duration
	// CaptchaTriggerThreshold 触发登录验证码所需的历史失败次数:
	// banned_ips.fail_count >= 阈值即要求验证码(无记录视为 0)。
	// 0 表示常驻要求验证码。
	CaptchaTriggerThreshold int
	// PullRateLimitPerHour 单 IP × 单订阅地址每小时允许的拉取次数
	// (pull-guard ticket 04,设置键 pull_rate_limit_per_hour)。
	// 0 表示关闭限频。
	PullRateLimitPerHour int
	// PullBlacklistEscalationCount 自动升级阈值(pull-guard ticket 05,设置键
	// pull_blacklist_escalation_count):同一 IP 在 1 小时窗口内累计
	// rate_limited 次数达此值,即自动写入 scope=sub 黑名单规则。
	// 必须为正数 —— 0 会让第一次超限就封禁,不作为"关闭"语义。
	PullBlacklistEscalationCount int
	// PullBlacklistDuration 自动黑名单规则时长(设置键 pull_blacklist_duration)。
	// 必须为正 —— 0 在规则表里是"永久",自动判定不该产生永久封禁。
	PullBlacklistDuration time.Duration
}

const (
	defaultBanThreshold = 5
	defaultBanDuration  = time.Hour
	// defaultCaptchaTriggerThreshold 默认一次失败即上验证码
	defaultCaptchaTriggerThreshold = 1
)

// loadSecurityPolicy 从设置读取 IP2Ban 策略，缺省时用默认值
func (s *Server) loadSecurityPolicy() securityPolicy {
	policy := securityPolicy{
		BanThreshold:                 defaultBanThreshold,
		BanDuration:                  defaultBanDuration,
		CaptchaTriggerThreshold:      defaultCaptchaTriggerThreshold,
		PullRateLimitPerHour:         defaultPullRateLimitPerHour,
		PullBlacklistEscalationCount: defaultPullBlacklistEscalationCount,
		PullBlacklistDuration:        defaultPullBlacklistDuration,
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
	// 验证码阈值允许 0(常驻要求),因此用 parseNonNegativeInt 而非 parsePositiveInt。
	if v := settings["captcha_trigger_threshold"]; v != "" {
		if n, err := parseNonNegativeInt(v); err == nil {
			policy.CaptchaTriggerThreshold = n
		}
	}
	// 拉取限频阈值允许 0(= 关闭限频),同样走 parseNonNegativeInt;
	// 解析失败(空值/负数/非数字)一律保留默认,不静默关闭防护。
	if v := settings["pull_rate_limit_per_hour"]; v != "" {
		if n, err := parseNonNegativeInt(v); err == nil {
			policy.PullRateLimitPerHour = n
		}
	}
	// 自动升级阈值与时长(ticket 05)只接受正值:两者的 0 都不是"关闭",
	// 而是"立刻封禁 / 永久封禁",解析失败或非正一律保留默认。
	if v := settings["pull_blacklist_escalation_count"]; v != "" {
		if n, err := parsePositiveInt(v); err == nil {
			policy.PullBlacklistEscalationCount = n
		}
	}
	if v := settings["pull_blacklist_duration"]; v != "" {
		if d, err := time.ParseDuration(strings.TrimSpace(v)); err == nil && d > 0 {
			policy.PullBlacklistDuration = d
		}
	}
	return policy
}

// failureReason labels which login stage produced a failure. It only shapes
// the log lines; the accounting is identical for every stage on purpose, so
// an attacker cannot pick a cheaper stage to brute force.
type failureReason string

const (
	failureReasonPassword failureReason = "password"
	failureReasonCaptcha  failureReason = "captcha"
	failureReasonMFA      failureReason = "mfa"
)

// chargeLoginFailure books one failed login attempt from ip against the IP2Ban
// counter and writes the threshold_ban audit row when this attempt is the one
// that crosses the threshold. It reports whether the IP is now banned so the
// caller can pick its own per-stage audit event (login_failure, captcha_failure,
// mfa_failure) accordingly.
//
// Single implementation on purpose: password, captcha and MFA failures share
// one counter and one threshold, so they must not drift apart.
func (s *Server) chargeLoginFailure(ip, username, userAgent string, policy securityPolicy, reason failureReason) bool {
	now := time.Now()
	nowBanned, err := s.st.RecordLoginFailure(ip, policy.BanThreshold, policy.BanDuration, now)
	if err != nil {
		s.logger.Error("record login failure failed", "ip", ip, "reason", string(reason), "error", err)
	}
	if !nowBanned {
		return false
	}
	bannedUntil := now.Add(policy.BanDuration)
	s.logger.Warn("ip banned after repeated failures", "ip", ip, "reason", string(reason))
	s.recordAudit("threshold_ban", ip, username,
		fmt.Sprintf("连续失败达阈值 %d，封禁至 %s",
			policy.BanThreshold, bannedUntil.Format("2006-01-02 15:04:05")),
		userAgent)
	return true
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

// parseNonNegativeInt 解析非负整数(0 是合法值)
func parseNonNegativeInt(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, strconv.ErrRange
	}
	return n, nil
}
