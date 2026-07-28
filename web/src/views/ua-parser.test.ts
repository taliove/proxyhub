// Unit tests for the UA parser: semantic labeling of browsers, mobile devices, and CLI tools
import { describe, it, expect } from 'vitest'
import { parseUserAgent } from './audit-utils'

describe('parseUserAgent', () => {
  it('returns "-" for undefined or empty UA', () => {
    expect(parseUserAgent(undefined)).toBe('-')
    expect(parseUserAgent('')).toBe('-')
  })

  it('parses CLI tools', () => {
    expect(parseUserAgent('curl/8.7.1')).toBe('curl/8.7.1')
    expect(parseUserAgent('curl/7.68.0')).toBe('curl/7.68.0')
    expect(parseUserAgent('Wget/1.21.2')).toBe('wget')
    expect(parseUserAgent('HTTPie/3.2.1')).toBe('HTTPie')
    expect(parseUserAgent('PostmanRuntime/7.32.3')).toBe('Postman')
  })

  it('parses Chrome on desktop', () => {
    const ua =
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36'
    expect(parseUserAgent(ua)).toBe('Chrome 138 · macOS')
  })

  it('parses Chrome on Windows', () => {
    const ua =
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36'
    expect(parseUserAgent(ua)).toBe('Chrome 120 · Windows 11')
  })

  it('parses Safari on macOS', () => {
    const ua =
      'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15'
    expect(parseUserAgent(ua)).toBe('Safari 17 · macOS')
  })

  it('parses Firefox on Linux', () => {
    const ua = 'Mozilla/5.0 (X11; Linux x86_64; rv:122.0) Gecko/20100101 Firefox/122.0'
    expect(parseUserAgent(ua)).toBe('Firefox 122 · Linux')
  })

  it('parses Edge', () => {
    const ua =
      'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0'
    expect(parseUserAgent(ua)).toBe('Edge 120 · Windows 11')
  })

  it('parses Safari on iPhone', () => {
    const ua =
      'Mozilla/5.0 (iPhone; CPU iPhone OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1'
    expect(parseUserAgent(ua)).toBe('Safari · iPhone')
  })

  it('parses Chrome on iPhone', () => {
    const ua =
      'Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) CriOS/120.0.6099.119 Mobile/15E148 Safari/604.1'
    expect(parseUserAgent(ua)).toBe('Chrome · iPhone')
  })

  it('parses Safari on iPad', () => {
    const ua =
      'Mozilla/5.0 (iPad; CPU OS 17_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Mobile/15E148 Safari/604.1'
    expect(parseUserAgent(ua)).toBe('Safari · iPad')
  })

  it('parses Chrome on Android', () => {
    const ua =
      'Mozilla/5.0 (Linux; Android 13; SM-S911B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.6099.144 Mobile Safari/537.36'
    expect(parseUserAgent(ua)).toBe('Chrome 120 · Android')
  })

  it('parses Firefox on Android', () => {
    const ua = 'Mozilla/5.0 (Android 13; Mobile; rv:122.0) Gecko/122.0 Firefox/122.0'
    expect(parseUserAgent(ua)).toBe('Firefox · Android')
  })

  it('truncates unrecognized UA to 50 characters', () => {
    const longUA =
      'SomeWeirdBot/1.0 with a very long description that exceeds fifty characters easily'
    const result = parseUserAgent(longUA)
    expect(result).toHaveLength(50)
    expect(result).toMatch(/\.\.\.$/)
  })

  it('returns short unrecognized UA as-is', () => {
    const shortUA = 'CustomBot/2.0'
    expect(parseUserAgent(shortUA)).toBe(shortUA)
  })
})
