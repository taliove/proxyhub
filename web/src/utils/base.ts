// 部署前缀(Site Path)。生产环境后端在 index.html 里注入
// window.__PH_BASE__(见 internal/server handleSPA);开发环境未注入,为空串,
// 一切行为与根路径部署一致。所有需要根绝对路径的地方一律经 appBase() 拼接,
// 禁止再手写 '/xxx' 根路径。
declare global {
  interface Window {
    __PH_BASE__?: string
  }
}

export const appBase = (): string => window.__PH_BASE__ ?? ''
