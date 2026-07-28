// 拉取守卫(pull-guard)的展示口径:拉取状态与 IP 规则字段的中英映射,
// 以及"时长选项 -> 请求体"的转换。单一事实源:IP 规则区块(views/IPRuleList.vue)
// 与订阅 IP 明细(components/IPStatsTable.vue)都取自这里。
//
// 后端口径:
//   - 拉取状态取值见 internal/store/pull_status.go(RecordPull 只接受闭集内的值)
//   - 规则 scope/source 取值见 internal/store 的 IPRuleScopeGlobal/Sub、
//     IPRuleSourceManual/Auto;审计文案(整站拒止/拉取黑名单)与
//     internal/server/handlers_iprules.go 的 ipRuleScopeLabel 对齐
//   - POST /api/admin/ip-rules 收 duration(Go duration 串)或 permanent=true,
//     两者都不给会 400,所以时长选项必须落到其中一种形状

export type GuardTagType = 'success' | 'info' | 'warning' | 'danger'

interface LabelMeta {
  label: string
  tag: GuardTagType
}

// PULL_STATUS_META 键即后端 pull_status.go 的闭集;未知值原样透出(向前兼容)。
const PULL_STATUS_META: Record<string, LabelMeta> = {
  // 真实投递成功
  ok: { label: '成功', tag: 'success' },
  // 触发限频:是配置生效的正常拦截,不是故障,用警告色
  rate_limited: { label: '限频', tag: 'warning' },
  // 地域白名单在强制模式下拒止
  geo_blocked: { label: '地域拦截', tag: 'danger' },
  // 观察模式:本该拒止但仍然投递了,与真实拦截区分开
  geo_would_block: { label: '地域观察', tag: 'warning' },
  // scope=sub 的 IP 规则命中
  blacklisted: { label: '黑名单', tag: 'danger' },
  // 订阅地址存在但已停用
  disabled: { label: '已禁用', tag: 'info' },
  // 未知 path 或 token 不匹配(对客户端统一 404)
  bad_token: { label: '错误令牌', tag: 'danger' }
}

/** pullStatusLabel 返回拉取状态的中文展示名;空值给"未知",未知取值原样返回。 */
export function pullStatusLabel(status: string): string {
  if (!status) return '未知'
  return PULL_STATUS_META[status]?.label || status
}

/** pullStatusTag 返回拉取状态对应的 el-tag 类型;空值/未知取值用中性色。 */
export function pullStatusTag(status: string): GuardTagType {
  return PULL_STATUS_META[status]?.tag || 'info'
}

// RULE_SCOPE_META 规则作用范围:global 拒止整站,sub 只掐订阅拉取。
const RULE_SCOPE_META: Record<string, LabelMeta> = {
  global: { label: '整站拒止', tag: 'danger' },
  sub: { label: '拉取黑名单', tag: 'warning' }
}

/** ruleScopeLabel 返回规则作用范围的中文名;未知取值原样返回。 */
export function ruleScopeLabel(scope: string): string {
  return RULE_SCOPE_META[scope]?.label || scope
}

/** ruleScopeTag 返回作用范围对应的 el-tag 类型;整站拒止更重,用危险色。 */
export function ruleScopeTag(scope: string): GuardTagType {
  return RULE_SCOPE_META[scope]?.tag || 'info'
}

/** RULE_SCOPE_OPTIONS 由 RULE_SCOPE_META 派生,新增表单的下拉选项。 */
export const RULE_SCOPE_OPTIONS: ReadonlyArray<{ label: string; value: string }> = Object.entries(
  RULE_SCOPE_META
).map(([value, meta]) => ({ value, label: meta.label }))

// RULE_SOURCE_META 规则来源:手动 = 管理员写的,自动 = 守卫按策略落的。
const RULE_SOURCE_META: Record<string, LabelMeta> = {
  manual: { label: '手动', tag: 'info' },
  auto: { label: '自动', tag: 'warning' }
}

/** ruleSourceLabel 返回规则来源的中文名;未知取值原样返回。 */
export function ruleSourceLabel(source: string): string {
  return RULE_SOURCE_META[source]?.label || source
}

/** ruleSourceTag 返回规则来源对应的 el-tag 类型。 */
export function ruleSourceTag(source: string): GuardTagType {
  return RULE_SOURCE_META[source]?.tag || 'info'
}

// RULE_DURATION_PERMANENT 时长选项里代表"永久"的哨兵值(与后端 duration 串区分开)。
export const RULE_DURATION_PERMANENT = 'permanent'

/** RULE_DURATION_OPTIONS 新增/封禁时可选的时长档位,与审计页封禁抽屉保持一致。 */
export const RULE_DURATION_OPTIONS: ReadonlyArray<{ label: string; value: string }> = [
  { label: '1 小时', value: '1h' },
  { label: '24 小时', value: '24h' },
  { label: '永久', value: RULE_DURATION_PERMANENT }
]

/** RuleWindow 是 POST /api/admin/ip-rules 的时长片段:两种形状二选一。 */
export type RuleWindow = { duration: string } | { permanent: true }

/**
 * ruleWindowPayload 把时长档位转成后端要的请求片段。
 * 永久走 permanent=true 而不是 duration="permanent",避免依赖后端对哨兵串的兼容。
 */
export function ruleWindowPayload(duration: string): RuleWindow {
  if (!duration || duration === RULE_DURATION_PERMANENT) return { permanent: true }
  return { duration }
}
