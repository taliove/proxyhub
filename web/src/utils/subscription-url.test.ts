import { describe, expect, it } from 'vitest'
import { clashInstallUrl, subscriptionUrl } from './subscription-url'
import type { Endpoint } from '@/types'

const ep = { id: 1, path: 'abc123', token: 'tok' } as Endpoint

describe('subscription-url', () => {
  it('builds the bare URL without format (UA 自动分流)', () => {
    expect(subscriptionUrl(ep)).toBe(`${window.location.origin}/sub/abc123?token=tok`)
  })

  it('appends explicit format params', () => {
    expect(subscriptionUrl(ep, 'base64')).toBe(
      `${window.location.origin}/sub/abc123?token=tok&format=base64`
    )
    expect(subscriptionUrl(ep, 'clash')).toBe(
      `${window.location.origin}/sub/abc123?token=tok&format=clash`
    )
  })

  it('builds the clash:// install link with an encoded format=clash URL', () => {
    const got = clashInstallUrl(ep)
    expect(got.startsWith('clash://install-config?url=')).toBe(true)
    const inner = decodeURIComponent(got.slice('clash://install-config?url='.length))
    expect(inner).toBe(subscriptionUrl(ep, 'clash'))
  })
})
