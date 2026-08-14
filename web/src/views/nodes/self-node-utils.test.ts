import { describe, expect, it } from 'vitest'
import { parseNodeUrl } from './self-node-utils'

// 分享链接 fixture:example.com + 全零 UUID(仓库测试纪律)
const ZERO_UUID = '00000000-0000-0000-0000-000000000000'

describe('parseNodeUrl vless reality(issue #90)', () => {
  it('security=reality 视为 TLS 且 reality 参数全量保留', () => {
    const url =
      `vless://${ZERO_UUID}@reality.example.com:443` +
      `?security=reality&sni=www.example.com&pbk=test-public-key&sid=ab12` +
      `&flow=xtls-rprx-vision&fp=chrome&type=tcp#测试Reality`
    const parsed = parseNodeUrl(url)
    expect(parsed).not.toBeNull()
    expect(parsed!.protocol).toBe('vless')
    expect(parsed!.server).toBe('reality.example.com')
    expect(parsed!.port).toBe(443)
    expect(parsed!.uuid).toBe(ZERO_UUID)
    expect(parsed!.name).toBe('测试Reality')
    // reality 必须置 TLS(此前被误判为非 TLS,造出明文坏节点)
    expect(parsed!.tls).toBe(true)
    // pbk/sid/flow/fp/sni 保真
    expect(parsed!.reality_public_key).toBe('test-public-key')
    expect(parsed!.reality_short_id).toBe('ab12')
    expect(parsed!.flow).toBe('xtls-rprx-vision')
    expect(parsed!.client_fingerprint).toBe('chrome')
    expect(parsed!.sni).toBe('www.example.com')
  })

  it('security=tls 置 TLS,reality 参数为空', () => {
    const url = `vless://${ZERO_UUID}@tls.example.com:443?security=tls&sni=sni.example.com&type=tcp`
    const parsed = parseNodeUrl(url)
    expect(parsed!.tls).toBe(true)
    expect(parsed!.sni).toBe('sni.example.com')
    expect(parsed!.reality_public_key).toBe('')
    expect(parsed!.flow).toBe('')
  })

  it('无 security(明文)不置 TLS,行为不变', () => {
    const url = `vless://${ZERO_UUID}@plain.example.com:8443?type=tcp`
    const parsed = parseNodeUrl(url)
    expect(parsed!.tls).toBe(false)
    expect(parsed!.reality_public_key).toBe('')
  })
})
