import type { SelfNode } from '@/types'

export type SelfNodeForm = Omit<SelfNode, 'id'>

// 每种协议需要的字段(server/port/name 为共有,不在此表)
export const PROTOCOL_FIELDS: Record<string, string[]> = {
  ss: ['cipher', 'password'],
  trojan: ['password'],
  vmess: ['uuid', 'alter_id', 'cipher', 'network', 'tls', 'grpc_service_name', 'grpc_authority'],
  vless: ['uuid', 'network', 'tls', 'grpc_service_name', 'grpc_authority']
}

export const emptyForm = (): SelfNodeForm => ({
  name: '',
  protocol: 'ss',
  server: '',
  port: 443,
  uuid: '',
  password: '',
  cipher: '',
  alter_id: 0,
  network: 'tcp',
  tls: false,
  grpc_service_name: '',
  grpc_authority: '',
  sni: '',
  flow: '',
  reality_public_key: '',
  reality_short_id: '',
  client_fingerprint: '',
  enabled: true
})

// 表单字段可见性:按协议决定;grpc 两字段仅在 network=grpc 且协议支持时显示
export const fieldVisible = (form: SelfNodeForm, field: string): boolean => {
  if (field === 'grpc_service_name' || field === 'grpc_authority') {
    return form.network === 'grpc' && (form.protocol === 'vmess' || form.protocol === 'vless')
  }
  return (PROTOCOL_FIELDS[form.protocol] || []).includes(field)
}

// 解析 vless:// URL
const parseVlessUrl = (url: string): Partial<SelfNodeForm> => {
  // vless://uuid@server:port?params#name
  const match = url.match(/^vless:\/\/([^@]+)@([^:]+):(\d+)(\?([^#]*))?(#(.*))?$/)
  if (!match) throw new Error('Invalid vless URL')

  const [, uuid, server, portStr, , query, , name] = match
  const port = parseInt(portStr)
  const params = new URLSearchParams(query || '')

  // security 判定(issue #90):reality 与 tls 一样必须置 TLS,且 reality 参数
  // (pbk/sid/flow/fp/sni)全量保留——丢失会静默造出明文 VLESS 坏节点(ADR 0043)。
  const security = params.get('security') || ''

  return {
    name: name ? decodeURIComponent(name) : `VLESS-${server}`,
    protocol: 'vless',
    server,
    port,
    uuid,
    network: params.get('type') || 'tcp',
    tls: security === 'tls' || security === 'reality',
    grpc_service_name: params.get('serviceName') || '',
    // authority 缺省回退 host(与后端解析层同口径:部分机场按 vmess host 约定承载)
    grpc_authority: params.get('authority') || params.get('host') || '',
    sni: params.get('sni') || '',
    flow: params.get('flow') || '',
    reality_public_key: params.get('pbk') || '',
    reality_short_id: params.get('sid') || '',
    client_fingerprint: params.get('fp') || '',
    enabled: true
  }
}

// 解析 vmess:// URL(base64 编码的 JSON)
const parseVmessUrl = (url: string): Partial<SelfNodeForm> => {
  const base64 = url.replace('vmess://', '')
  const json = JSON.parse(atob(base64))

  return {
    name: json.ps || `VMess-${json.add}`,
    protocol: 'vmess',
    server: json.add,
    port: parseInt(json.port),
    uuid: json.id,
    alter_id: parseInt(json.aid || '0'),
    cipher: json.scy || 'auto',
    network: json.net || 'tcp',
    tls: json.tls === 'tls',
    grpc_service_name: json.path || '',
    // vmess JSON:net=grpc 时 host 即 grpc authority(v2rayN 约定)
    grpc_authority: json.net === 'grpc' ? json.host || '' : '',
    enabled: true
  }
}

// 解析 ss:// URL
const parseSsUrl = (url: string): Partial<SelfNodeForm> => {
  // ss://base64(method:password)@server:port#name
  const match = url.match(/^ss:\/\/([^@]+)@([^:]+):(\d+)(#(.*))?$/)
  if (!match) throw new Error('Invalid ss URL')

  const [, encoded, server, portStr, , name] = match
  const decoded = atob(encoded)
  const [cipher, password] = decoded.split(':')

  return {
    name: name ? decodeURIComponent(name) : `SS-${server}`,
    protocol: 'ss',
    server,
    port: parseInt(portStr),
    cipher,
    password,
    enabled: true
  }
}

// 解析 trojan:// URL
const parseTrojanUrl = (url: string): Partial<SelfNodeForm> => {
  // trojan://password@server:port#name
  const match = url.match(/^trojan:\/\/([^@]+)@([^:]+):(\d+)(#(.*))?$/)
  if (!match) throw new Error('Invalid trojan URL')

  const [, password, server, portStr, , name] = match

  return {
    name: name ? decodeURIComponent(name) : `Trojan-${server}`,
    protocol: 'trojan',
    server,
    port: parseInt(portStr),
    password,
    enabled: true
  }
}

// 按协议前缀分发到对应解析器;无法识别或解析失败返回 null
export const parseNodeUrl = (url: string): Partial<SelfNodeForm> | null => {
  try {
    if (url.startsWith('vless://')) return parseVlessUrl(url)
    if (url.startsWith('vmess://')) return parseVmessUrl(url)
    if (url.startsWith('ss://')) return parseSsUrl(url)
    if (url.startsWith('trojan://')) return parseTrojanUrl(url)
    return null
  } catch (e) {
    console.error('Parse error:', e)
    return null
  }
}
