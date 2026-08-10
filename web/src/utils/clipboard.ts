// copyText 复制文本到剪贴板,带降级:
// navigator.clipboard.writeText 只在安全上下文(https 或 localhost)可用;
// 局域网 http 部署(如 http://192.168.x.x)下 clipboard 为 undefined,
// 退回 textarea + execCommand('copy') 老路径。
export const copyText = async (text: string): Promise<void> => {
  if (navigator.clipboard?.writeText) {
    await navigator.clipboard.writeText(text)
    return
  }
  const ta = document.createElement('textarea')
  ta.value = text
  // 防页面滚动/选中态闪烁
  ta.style.position = 'fixed'
  ta.style.opacity = '0'
  document.body.appendChild(ta)
  ta.select()
  try {
    const ok = document.execCommand('copy')
    if (!ok) throw new Error('execCommand copy failed')
  } finally {
    document.body.removeChild(ta)
  }
}
