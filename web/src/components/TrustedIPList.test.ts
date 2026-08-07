import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import TrustedIPList from './TrustedIPList.vue'
import * as api from '@/api/trusted-ips'

// 受信 IP 管理区块(ticket 10):列表呈现 + 撤销 + 推荐一键采纳 + 自动信任开关。
// API 层整体打桩:线上契约由后端 handler 测试守,这里只验证 UI 接线与副作用。
vi.mock('@/api/trusted-ips', () => ({
  listTrustedIPs: vi.fn(),
  trustIP: vi.fn(),
  revokeTrustedIP: vi.fn(),
  setAutoTrustIP: vi.fn()
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

// el-table / el-table-column 桩:按行渲染 scoped slot,并渲染列头 label;
// 无 default 插槽的 prop 列按 row[prop] 渲染(与真实 el-table 一致)。
const ElTableStub = defineComponent({
  name: 'ElTable',
  props: { data: { type: Array, default: () => [] } },
  setup(props, { slots }) {
    provide('rows', toRef(props, 'data'))
    return () => h('div', { class: 'el-table-stub' }, slots.default?.())
  }
})
const ElTableColumnStub = defineComponent({
  name: 'ElTableColumn',
  props: { label: { type: String, default: '' }, prop: { type: String, default: '' } },
  setup(props, { slots }) {
    const rows = inject<Ref<unknown[]>>('rows')!
    const cellOf = (row: unknown) =>
      slots.default
        ? slots.default({ row })
        : String((row as Record<string, unknown>)[props.prop] ?? '')
    return () =>
      h('div', { class: 'el-column-stub' }, [
        h('div', { class: 'tc-label' }, props.label),
        ...rows.value.map((row, i) => h('div', { class: 'tc-row', key: i }, cellOf(row)))
      ])
  }
})
const ElSwitchStub = defineComponent({
  name: 'ElSwitch',
  props: { modelValue: { type: Boolean, default: false } },
  emits: ['update:modelValue', 'change'],
  setup(props, { emit }) {
    return () =>
      h('button', {
        class: 'el-switch-stub',
        'data-on': String(props.modelValue),
        onClick: () => {
          emit('update:modelValue', !props.modelValue)
          emit('change', !props.modelValue)
        }
      })
  }
})
const ElButtonStub = defineComponent({
  name: 'ElButton',
  props: { type: { type: String, default: '' } },
  emits: ['click'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'button',
        { class: `el-button-stub btn-${props.type}`, onClick: () => emit('click') },
        slots.default?.()
      )
  }
})
const SimpleStub = (name: string, cls: string) =>
  defineComponent({
    name,
    props: { title: { type: String, default: '' }, description: { type: String, default: '' } },
    setup(props, { slots }) {
      return () => h('div', { class: cls }, [props.title || props.description, slots.default?.()])
    }
  })

const mountList = () =>
  mount(TrustedIPList, {
    global: {
      directives: { loading: {} },
      stubs: {
        'el-table': ElTableStub,
        'el-table-column': ElTableColumnStub,
        'el-switch': ElSwitchStub,
        'el-button': ElButtonStub,
        'el-alert': SimpleStub('ElAlert', 'el-alert-stub'),
        'el-empty': SimpleStub('ElEmpty', 'el-empty-stub'),
        'el-tag': SimpleStub('ElTag', 'el-tag-stub')
      }
    }
  })

const envelope = (over: Partial<api.TrustedIPsEnvelope> = {}): api.TrustedIPsEnvelope => ({
  trusted: [],
  recommendations: [],
  auto_trust_ip: false,
  threshold: 3,
  ...over
})

const grant = (over: Partial<api.TrustedIP> = {}): api.TrustedIP => ({
  ip: '203.0.113.7',
  expires_at: '2026-08-20T10:00:00Z',
  last_used_at: '2026-07-21T10:00:00Z',
  expired: false,
  region_code: 'HK',
  region_name: '香港',
  ...over
})

describe('TrustedIPList', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染受信 IP 的地理、最后使用与到期，并标记已过期条目', async () => {
    vi.mocked(api.listTrustedIPs).mockResolvedValue(
      envelope({
        trusted: [
          grant(),
          grant({ ip: '198.51.100.9', expired: true, region_name: '', region_code: '' })
        ]
      })
    )

    const wrapper = mountList()
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('203.0.113.7')
    expect(text).toContain('香港')
    expect(text).toContain('198.51.100.9')
    // 无地理记录降级为「未知」,过期行带标记
    expect(text).toContain('未知')
    expect(text).toContain('已过期')
  })

  it('撤销后调用 revokeTrustedIP 并重新拉取列表', async () => {
    vi.mocked(api.listTrustedIPs).mockResolvedValue(envelope({ trusted: [grant()] }))
    vi.mocked(api.revokeTrustedIP).mockResolvedValue({ ok: true, ip: '203.0.113.7' })

    const wrapper = mountList()
    await flushPromises()

    await wrapper.find('.btn-danger').trigger('click')
    await flushPromises()

    expect(vi.mocked(api.revokeTrustedIP)).toHaveBeenCalledWith('203.0.113.7')
    expect(vi.mocked(api.listTrustedIPs)).toHaveBeenCalledTimes(2)
  })

  it('推荐列表展示 MFA 成功次数，一键信任调用 trustIP', async () => {
    vi.mocked(api.listTrustedIPs).mockResolvedValue(
      envelope({
        recommendations: [
          { ip: '203.0.113.20', mfa_successes: 4, region_code: 'JP', region_name: '日本' }
        ]
      })
    )
    vi.mocked(api.trustIP).mockResolvedValue({ ok: true, ip: '203.0.113.20' })

    const wrapper = mountList()
    await flushPromises()

    expect(wrapper.text()).toContain('203.0.113.20')
    expect(wrapper.text()).toContain('4')

    await wrapper.find('.btn-primary').trigger('click')
    await flushPromises()

    expect(vi.mocked(api.trustIP)).toHaveBeenCalledWith('203.0.113.20')
    expect(vi.mocked(api.listTrustedIPs)).toHaveBeenCalledTimes(2)
  })

  it('自动信任开关默认关，切换后调用 setAutoTrustIP', async () => {
    vi.mocked(api.listTrustedIPs).mockResolvedValue(envelope())
    vi.mocked(api.setAutoTrustIP).mockResolvedValue({ ok: true, auto_trust_ip: true })

    const wrapper = mountList()
    await flushPromises()

    const sw = wrapper.find('.el-switch-stub')
    expect(sw.attributes('data-on')).toBe('false')

    await sw.trigger('click')
    await flushPromises()

    expect(vi.mocked(api.setAutoTrustIP)).toHaveBeenCalledWith(true)
    expect(wrapper.find('.el-switch-stub').attributes('data-on')).toBe('true')
  })

  it('开关保存失败时回滚本地状态（不谎报已开启）', async () => {
    vi.mocked(api.listTrustedIPs).mockResolvedValue(envelope())
    vi.mocked(api.setAutoTrustIP).mockRejectedValue(new Error('boom'))

    const wrapper = mountList()
    await flushPromises()

    await wrapper.find('.el-switch-stub').trigger('click')
    await flushPromises()

    expect(wrapper.find('.el-switch-stub').attributes('data-on')).toBe('false')
  })

  it('后端回报 auto_trust_ip=true 时开关初始为开', async () => {
    vi.mocked(api.listTrustedIPs).mockResolvedValue(envelope({ auto_trust_ip: true }))

    const wrapper = mountList()
    await flushPromises()

    expect(wrapper.find('.el-switch-stub').attributes('data-on')).toBe('true')
  })
})
