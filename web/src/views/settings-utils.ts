// 设置页的纯校验/归一化助手,与 .vue 分离以便不挂载 Element Plus 就能单测
// (同 views/login-utils.ts、views/mfa-enroll-utils.ts 的拆法)。

// CAPTCHA_TRIGGER_THRESHOLD_DEFAULT 与后端 defaultCaptchaTriggerThreshold
// 对齐(internal/server/security.go):默认 1,即一次登录失败即上验证码。
export const CAPTCHA_TRIGGER_THRESHOLD_DEFAULT = 1

// 上限纯前端约束:阈值再大等于把验证码关掉,交互上应引导用户改用其它手段
// 而不是填一个天文数字。后端只做非负校验,不设上限。
export const CAPTCHA_TRIGGER_THRESHOLD_MAX = 20

/**
 * validateCaptchaTriggerThreshold 校验验证码触发次数:非负整数。
 * 返回错误文案,合法时返回 null(0 合法 = 每次登录都要求验证码)。
 *
 * 后端 loadSecurityPolicy 对非法值是静默回落默认值,前端不该把"填错了但看起来
 * 保存成功"留给用户,所以保存前先在这里挡住。
 */
export function validateCaptchaTriggerThreshold(value: unknown): string | null {
  const raw = typeof value === 'string' ? value.trim() : value
  if (raw === '' || raw === null || raw === undefined) {
    return '验证码触发次数不能为空'
  }
  const n = Number(raw)
  if (!Number.isFinite(n) || !Number.isInteger(n)) {
    return '验证码触发次数必须是整数'
  }
  if (n < 0) {
    return '验证码触发次数不能为负数'
  }
  if (n > CAPTCHA_TRIGGER_THRESHOLD_MAX) {
    return `验证码触发次数不能大于 ${CAPTCHA_TRIGGER_THRESHOLD_MAX}`
  }
  return null
}
