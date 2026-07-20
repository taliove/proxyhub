// "HH:MM" 字符串 <-> Date 转换,供 el-time-picker 双向绑定。
// 只用日期的时分部分(日期基准无意义,固定到一个锚点)。

const ANCHOR = '2000-01-01'

// hhmmToDate 把 "HH:MM" 解析为锚点日期上的 Date;非法输入回落到 03:30。
export function hhmmToDate(hhmm: string): Date {
  const [h, m] = splitHhmm(hhmm) ?? [3, 30]
  return new Date(`${ANCHOR}T${pad2(h)}:${pad2(m)}:00`)
}

// dateToHhmm 把 Date 的时分格式化为零填充 "HH:MM"。
export function dateToHhmm(d: Date): string {
  if (!(d instanceof Date) || Number.isNaN(d.getTime())) return '03:30'
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

// splitHhmm 校验并拆分 "HH:MM";非法返回 null。
function splitHhmm(hhmm: string): [number, number] | null {
  if (typeof hhmm !== 'string' || hhmm.length !== 5 || hhmm[2] !== ':') return null
  const h = Number(hhmm.slice(0, 2))
  const m = Number(hhmm.slice(3, 5))
  if (!Number.isInteger(h) || !Number.isInteger(m)) return null
  if (h < 0 || h > 23 || m < 0 || m > 59) return null
  return [h, m]
}

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}
