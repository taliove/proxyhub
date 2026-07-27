// extractErrorDetail 从 axios 风格错误中提取后端错误详情。
// 后端错误字段不统一:多数处理器写 {message},用户管理面与模板库写 {error}
// (见 api/client.ts 拦截器的同款注释)。
export function extractErrorDetail(e: unknown): string | undefined {
  const body = (e as { response?: { data?: { message?: string; error?: string } } })?.response?.data
  return body?.message || body?.error || undefined
}
