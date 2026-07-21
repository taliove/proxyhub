import { describe, expect, test } from 'vitest'
import { tagLabel } from './taglabels'

describe('tagLabel', () => {
  describe('fixed tags - unlock capabilities', () => {
    test('nf-full -> Netflix全解', () => {
      expect(tagLabel('nf-full')).toBe('Netflix全解')
    })

    test('nf-originals -> Netflix仅自制', () => {
      expect(tagLabel('nf-originals')).toBe('Netflix仅自制')
    })

    test('yt-premium -> YouTube Premium', () => {
      expect(tagLabel('yt-premium')).toBe('YouTube Premium')
    })

    test('disney-plus -> Disney+', () => {
      expect(tagLabel('disney-plus')).toBe('Disney+')
    })

    test('openai -> OpenAI', () => {
      expect(tagLabel('openai')).toBe('OpenAI')
    })

    test('claude -> Claude', () => {
      expect(tagLabel('claude')).toBe('Claude')
    })

    test('gemini -> Gemini', () => {
      expect(tagLabel('gemini')).toBe('Gemini')
    })
  })

  describe('fixed tags - stability levels', () => {
    test('stable-good -> 稳定·优', () => {
      expect(tagLabel('stable-good')).toBe('稳定·优')
    })

    test('stable-fair -> 稳定·良', () => {
      expect(tagLabel('stable-fair')).toBe('稳定·良')
    })

    test('stable-poor -> 稳定·差', () => {
      expect(tagLabel('stable-poor')).toBe('稳定·差')
    })
  })

  describe('fixed tags - egress/quality', () => {
    test('fast -> 高速', () => {
      expect(tagLabel('fast')).toBe('高速')
    })

    test('ipv6 -> IPv6', () => {
      expect(tagLabel('ipv6')).toBe('IPv6')
    })

    test('residential -> 住宅', () => {
      expect(tagLabel('residential')).toBe('住宅')
    })

    test('hosting -> 机房', () => {
      expect(tagLabel('hosting')).toBe('机房')
    })

    test('dns-leak -> DNS泄露', () => {
      expect(tagLabel('dns-leak')).toBe('DNS泄露')
    })
  })

  describe('dynamic region tags', () => {
    test('region:US -> 美国', () => {
      expect(tagLabel('region:US')).toBe('美国')
    })

    test('region:CN -> 中国', () => {
      expect(tagLabel('region:CN')).toBe('中国')
    })

    test('region:HK -> 香港', () => {
      expect(tagLabel('region:HK')).toBe('香港')
    })

    test('region:JP -> 日本', () => {
      expect(tagLabel('region:JP')).toBe('日本')
    })

    test('region:SG -> 新加坡', () => {
      expect(tagLabel('region:SG')).toBe('新加坡')
    })

    test('region:UK -> 英国', () => {
      expect(tagLabel('region:UK')).toBe('英国')
    })

    test('region with lowercase code normalizes to uppercase', () => {
      expect(tagLabel('region:us')).toBe('美国')
      expect(tagLabel('region:cn')).toBe('中国')
    })

    test('unknown region code returns original code', () => {
      expect(tagLabel('region:XX')).toBe('XX')
      expect(tagLabel('region:ZZ')).toBe('ZZ')
    })
  })

  describe('unknown tags', () => {
    test('unknown tag returns as-is', () => {
      expect(tagLabel('unknown-tag')).toBe('unknown-tag')
      expect(tagLabel('future-feature')).toBe('future-feature')
    })

    test('empty string returns as-is', () => {
      expect(tagLabel('')).toBe('')
    })
  })
})
