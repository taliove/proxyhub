// 命名模式展示口径(订阅列表与详情抽屉共用):''=跟随全局, on=强制开, off=强制关(见 ADR 0012)。
type NameModeTagType = 'success' | 'info' | 'warning'

export const nameModeLabel = (mode: string): string =>
  mode === 'on' ? '强制开' : mode === 'off' ? '强制关' : '跟随全局'

export const nameModeTag = (mode: string): NameModeTagType =>
  mode === 'on' ? 'success' : mode === 'off' ? 'info' : 'warning'
