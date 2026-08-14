// 订阅地址 URL 拼装(issue #123):统一入口,Endpoints 页与主页快捷订阅共用。
// 订阅地址挂根命名空间 /sub(issue #74):不含 Site Path,链接进客户端/日志不泄露管理面路径。
import type { Endpoint } from '@/types'

// 显式格式参数(术语见 CONTEXT.md「订阅格式」);v2ray 别名不暴露给新 UI。
export type SubscriptionFormat = 'clash' | 'base64'

// subscriptionUrl 拼装完整订阅 URL。format 缺省 = 不带参数,
// 由 UA 分流自动判定(ADR 0049);显式 format 永远优先于 UA。
export function subscriptionUrl(row: Endpoint, format?: SubscriptionFormat): string {
  const base = `${window.location.origin}/sub/${row.path}?token=${row.token}`
  return format ? `${base}&format=${format}` : base
}

// clashInstallUrl Clash 系客户端一键导入 scheme:显式 format=clash 保证
// 拿到 YAML(不依赖 UA 碰巧命中),整体 url-encode 进 url 参数。
export function clashInstallUrl(row: Endpoint): string {
  return `clash://install-config?url=${encodeURIComponent(subscriptionUrl(row, 'clash'))}`
}
