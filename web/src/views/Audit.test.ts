// Component tests for the audit page (ticket 11): the event-type filter offers
// the login-hardening events, the list renders their Chinese labels, and a
// login_success row shows which second factor (or skip) let it through.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, inject, provide, toRef, type Ref } from 'vue'
import Audit from './Audit.vue'
import client from '@/api/client'

vi.mock('@/api/client', () => ({
  default: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn()
  }
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() }
}))

interface AuditRow {
  event_type: string
  ip: string
  username: string
  detail: string
  created_at: string
}

// Table stubs render every row through the column scoped slots.
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
  props: {
    label: { type: String, default: '' },
    prop: { type: String, default: '' }
  },
  setup(props, { slots }) {
    const rows = inject<Ref<unknown[]>>('rows')!
    return () =>
      h('div', { class: 'el-column-stub' }, [
        h('div', { class: 'tc-label' }, props.label),
        ...rows.value.map((row, i) =>
          h('div', { class: 'tc-row', key: i }, [
            slots.default
              ? slots.default({ row })
              : String((row as Record<string, unknown>)[props.prop] ?? '')
          ])
        )
      ])
  }
})
// el-select stub renders its options so the filter list is assertable.
const ElSelectStub = defineComponent({
  name: 'ElSelect',
  props: { modelValue: { type: [String, Array], default: '' } },
  setup(_, { slots }) {
    return () => h('div', { class: 'el-select-stub' }, slots.default?.())
  }
})
const ElOptionStub = defineComponent({
  name: 'ElOption',
  props: {
    label: { type: String, default: '' },
    value: { type: String, default: '' }
  },
  setup(props) {
    return () => h('div', { class: 'el-option-stub', 'data-value': props.value }, props.label)
  }
})
const ElTagStub = defineComponent({
  name: 'ElTag',
  props: { type: { type: String, default: '' } },
  setup(props, { slots }) {
    return () => h('span', { class: 'el-tag-stub', 'data-type': props.type }, slots.default?.())
  }
})
const SimpleSlotStub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: `${name}-stub` }, [slots.header?.(), slots.default?.()])
    }
  })

const row = (over: Partial<AuditRow> = {}): AuditRow => ({
  event_type: 'login_success',
  ip: '203.0.113.10',
  username: 'alice',
  detail: '',
  created_at: '2026-07-20T08:00:00Z',
  ...over
})

const mountView = (events: AuditRow[] = []) => {
  vi.mocked(client.get).mockImplementation((async (url: string) =>
    String(url).startsWith('/audit/banned')
      ? { banned: [] }
      : { events, total: events.length }) as never)
  return mount(Audit, {
    global: {
      directives: { loading: {} },
      stubs: {
        PageHeader: SimpleSlotStub('PageHeader'),
        'el-card': SimpleSlotStub('ElCard'),
        'el-table': ElTableStub,
        'el-table-column': ElTableColumnStub,
        'el-select': ElSelectStub,
        'el-option': ElOptionStub,
        'el-tag': ElTagStub,
        'el-input': SimpleSlotStub('ElInput'),
        'el-button': SimpleSlotStub('ElButton'),
        'el-pagination': SimpleSlotStub('ElPagination'),
        'el-empty': SimpleSlotStub('ElEmpty')
      }
    }
  })
}

// The time-range select shares the el-option stub, so filter assertions look at
// the first select only.
const eventFilterLabels = (wrapper: ReturnType<typeof mountView>) =>
  wrapper
    .findAll('.el-select-stub')[0]
    .findAll('.el-option-stub')
    .map((o) => o.text())

describe('Audit view', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('offers the four login-hardening events in the type filter', async () => {
    const wrapper = mountView()
    await flushPromises()

    const labels = eventFilterLabels(wrapper)
    expect(labels).toContain('验证码失败')
    expect(labels).toContain('MFA 绑定')
    expect(labels).toContain('MFA 失败')
    expect(labels).toContain('MFA 重置')
    // pre-existing options survive
    expect(labels).toContain('登录成功')
    expect(labels).toContain('蜜罐封禁')
  })

  it('sends the selected new types through the event_type query param', async () => {
    const wrapper = mountView()
    await flushPromises()

    const vm = wrapper.vm as unknown as {
      filterTypes: string[]
      reload: () => Promise<void>
    }
    vm.filterTypes = ['captcha_failure', 'mfa_failure']
    await vm.reload()
    await flushPromises()

    const url = String(vi.mocked(client.get).mock.calls.at(-1)![0])
    expect(url).toContain('event_type=captcha_failure%2Cmfa_failure')
  })

  it('renders the new event types with Chinese labels in the list', async () => {
    const wrapper = mountView([
      row({ event_type: 'captcha_failure', detail: '缺少验证码' }),
      row({ event_type: 'mfa_enrolled', detail: 'totp enabled, 10 recovery codes issued' }),
      row({ event_type: 'mfa_failure', detail: 'mfa verification failed, pending=abcd1234' }),
      row({ event_type: 'mfa_reset', detail: 'mfa reset for user id=2 by super admin' })
    ])
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('验证码失败')
    expect(text).toContain('MFA 绑定')
    expect(text).toContain('MFA 失败')
    expect(text).toContain('MFA 重置')
    // details still readable alongside the labels
    expect(text).toContain('缺少验证码')
    expect(text).toContain('pending=abcd1234')
  })

  it('badges a login_success by which second factor passed', async () => {
    const wrapper = mountView([
      row({ detail: 'mfa=totp' }),
      row({ detail: 'mfa=recovery' }),
      row({ detail: 'mfa_skipped=trusted_ip' })
    ])
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('TOTP')
    expect(text).toContain('恢复码')
    expect(text).toContain('受信 IP 免验')
    // rendered as tags (Element Plus styling), not bare text
    const badges = wrapper.findAll('.mfa-badge')
    expect(badges.map((b) => b.text())).toEqual(['TOTP', '恢复码', '受信 IP 免验'])
    expect(badges.map((b) => b.attributes('data-type'))).toEqual(['success', 'warning', 'info'])
    // the marker is shown as a badge, not repeated as raw detail text
    expect(text).not.toContain('mfa=totp')
    expect(text).not.toContain('mfa_skipped=trusted_ip')
  })

  it('shows no mfa badge for a login_success without a marker', async () => {
    const wrapper = mountView([row({ detail: '' })])
    await flushPromises()

    const text = wrapper.text()
    expect(text).toContain('登录成功')
    expect(text).not.toContain('TOTP')
    expect(text).not.toContain('受信 IP 免验')
  })
})
