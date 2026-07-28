// IP 访问规则 API 封装(pull-guard ticket 02 后端 internal/server/handlers_iprules.go)。
//
// 线上契约(全部 adminGuard,超管专属):
//   GET    /api/admin/ip-rules[?scope=global|sub] -> {rules: IPRule[]}
//   POST   /api/admin/ip-rules                   -> IPRule
//          body: {ip_or_cidr, scope, duration|permanent, comment}
//   DELETE /api/admin/ip-rules/{id}              -> {success: true}
//   POST   /api/admin/ip-rules/{id}/promote      -> IPRule   仅 scope=sub 可升级
//
// 同一 (target, scope) 重复 POST 是滑动窗口而非报错,所以"续期"就是再提交一次。
import client from './client'
import { ruleWindowPayload } from '@/utils/pullguard'

// IPRule 一条访问规则。expires_at 为 null 表示永久;expired 让 UI 能把已失效的行
// 标灰而不是假装仍在生效(后端刻意保留过期行,便于操作者看见并清理)。
export interface IPRule {
  id: number
  ip_or_cidr: string
  scope: string
  source: string
  expires_at: string | null
  expired: boolean
  permanent: boolean
  comment: string
  created_at: string
}

// listIPRules 读取规则列表;scope 为空表示不过滤。
export function listIPRules(scope = ''): Promise<{ rules: IPRule[] }> {
  const query = scope ? `?scope=${encodeURIComponent(scope)}` : ''
  return client.get<unknown, { rules: IPRule[] }>(`/admin/ip-rules${query}`)
}

// CreateIPRuleInput 新增规则的入参;duration 走 RULE_DURATION_OPTIONS 的档位值
// ("1h"/"24h"/"permanent"),转请求体的逻辑收在 ruleWindowPayload。
export interface CreateIPRuleInput {
  ip_or_cidr: string
  scope: string
  duration: string
  comment?: string
}

// createIPRule 新增一条手动规则(后端把 source 固定写成 manual)。
export function createIPRule(input: CreateIPRuleInput): Promise<IPRule> {
  return client.post<unknown, IPRule>('/admin/ip-rules', {
    ip_or_cidr: input.ip_or_cidr,
    scope: input.scope,
    comment: input.comment || '',
    ...ruleWindowPayload(input.duration)
  })
}

// deleteIPRule 删除一条规则(删掉即恢复访问)。
export function deleteIPRule(id: number): Promise<{ success: boolean }> {
  return client.delete<unknown, { success: boolean }>(`/admin/ip-rules/${id}`)
}

// promoteIPRule 把拉取黑名单(scope=sub)升级为整站拒止(scope=global)。
export function promoteIPRule(id: number): Promise<IPRule> {
  return client.post<unknown, IPRule>(`/admin/ip-rules/${id}/promote`, {})
}
