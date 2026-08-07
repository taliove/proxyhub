import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import type { Endpoint } from '@/types'
import EndpointGeoConfigSection from './EndpointGeoConfigSection.vue'
import * as endpointsApi from '@/api/endpoints'

// Mock API client
vi.mock('@/api/endpoints', () => ({
  updateEndpointGeoConfig: vi.fn()
}))

vi.mock('element-plus', () => ({
  ElMessage: { success: vi.fn(), error: vi.fn() }
}))

// Element Plus 组件测试桩
const ElFormStub = defineComponent({
  name: 'ElForm',
  setup(_, { slots }) {
    return () => h('div', { class: 'el-form-stub' }, slots.default?.())
  }
})
const ElFormItemStub = defineComponent({
  name: 'ElFormItem',
  props: { label: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      h('div', { class: 'el-form-item-stub' }, [
        h('label', { class: 'form-label' }, props.label),
        slots.default?.()
      ])
  }
})
const ElRadioGroupStub = defineComponent({
  name: 'ElRadioGroup',
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'div',
        {
          class: 'el-radio-group-stub',
          'data-value': props.modelValue,
          onClick: () => emit('update:modelValue', 'observe')
        },
        slots.default?.()
      )
  }
})
const ElRadioButtonStub = defineComponent({
  name: 'ElRadioButton',
  props: { label: { type: String, default: '' } },
  setup(props, { slots }) {
    return () =>
      h('button', { class: 'el-radio-button-stub', 'data-label': props.label }, slots.default?.())
  }
})
const ElSelectStub = defineComponent({
  name: 'ElSelect',
  props: { modelValue: { type: Array, default: () => [] } },
  emits: ['update:modelValue'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'div',
        {
          class: 'el-select-stub',
          'data-values': JSON.stringify(props.modelValue),
          onClick: () => emit('update:modelValue', ['CN', 'US'])
        },
        slots.default?.()
      )
  }
})
const ElOptionStub = defineComponent({
  name: 'ElOption',
  props: { label: { type: String }, value: { type: String } },
  setup(props) {
    return () =>
      h('div', { class: 'el-option-stub', 'data-value': props.value }, props.label || props.value)
  }
})
const ElButtonStub = defineComponent({
  name: 'ElButton',
  props: { loading: { type: Boolean, default: false } },
  emits: ['click'],
  setup(props, { slots, emit }) {
    return () =>
      h(
        'button',
        { class: 'el-button-stub', disabled: props.loading, onClick: () => emit('click') },
        slots.default?.()
      )
  }
})
const ElCollapseStub = defineComponent({
  name: 'ElCollapse',
  setup(_, { slots }) {
    return () => h('div', { class: 'el-collapse-stub' }, slots.default?.())
  }
})
const ElCollapseItemStub = defineComponent({
  name: 'ElCollapseItem',
  setup(_, { slots }) {
    return () => h('div', { class: 'el-collapse-item-stub' }, slots.default?.())
  }
})
const ElAlertStub = defineComponent({
  name: 'ElAlert',
  setup(_, { slots }) {
    return () => h('div', { class: 'el-alert-stub' }, slots.default?.())
  }
})
const ElInputStub = defineComponent({
  name: 'ElInput',
  props: { modelValue: { type: String, default: '' } },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    return () =>
      h('input', {
        class: 'el-input-stub',
        value: props.modelValue,
        onInput: (e: Event) => emit('update:modelValue', (e.target as HTMLInputElement).value)
      })
  }
})
const ElIconStub = defineComponent({
  name: 'ElIcon',
  setup(_, { slots }) {
    return () => h('span', { class: 'el-icon-stub' }, slots.default?.())
  }
})

const endpoint: Endpoint = {
  id: 1,
  alias: '测试订阅',
  path: '/sub/test',
  token: 'token123',
  enabled: true,
  created_at: '2026-01-01T00:00:00Z',
  name_mode: '',
  name_template: '',
  conditions: '',
  template_name: '',
  public_name: '',
  geo_mode: 'off',
  geo_countries: '',
  geo_provinces: ''
}

const mountSection = (ep: Endpoint = endpoint) => {
  return mount(EndpointGeoConfigSection, {
    props: { endpoint: ep },
    global: {
      stubs: {
        'el-form': ElFormStub,
        'el-form-item': ElFormItemStub,
        'el-radio-group': ElRadioGroupStub,
        'el-radio-button': ElRadioButtonStub,
        'el-select': ElSelectStub,
        'el-option': ElOptionStub,
        'el-button': ElButtonStub,
        'el-collapse': ElCollapseStub,
        'el-collapse-item': ElCollapseItemStub,
        'el-alert': ElAlertStub,
        'el-input': ElInputStub,
        'el-icon': ElIconStub,
        WarningFilled: { template: '<span>!</span>' }
      }
    }
  })
}

describe('EndpointGeoConfigSection', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('渲染三档模式选择：关闭/观察/拦截', () => {
    const wrapper = mountSection()
    const text = wrapper.text()
    expect(text).toContain('关闭')
    expect(text).toContain('观察')
    expect(text).toContain('拦截')
  })

  it('初始状态从端点 geo_mode 回显，默认 off', () => {
    const wrapper = mountSection()
    const radioGroup = wrapper.find('.el-radio-group-stub')
    expect(radioGroup.attributes('data-value')).toBe('off')
  })

  it('端点 geo_mode=observe 时回显为观察档', () => {
    const wrapper = mountSection({ ...endpoint, geo_mode: 'observe' })
    const radioGroup = wrapper.find('.el-radio-group-stub')
    expect(radioGroup.attributes('data-value')).toBe('observe')
  })

  it('端点 geo_countries 回显为国家数组', () => {
    const wrapper = mountSection({
      ...endpoint,
      geo_mode: 'enforce',
      geo_countries: 'CN,US,JP'
    })
    const select = wrapper.find('.el-select-stub')
    expect(JSON.parse(select.attributes('data-values') || '[]')).toEqual(['CN', 'US', 'JP'])
  })

  it('模式为 off 时不显示国家和省份选择器', () => {
    const wrapper = mountSection({ ...endpoint, geo_mode: 'off' })
    expect(wrapper.findAll('.el-select-stub')).toHaveLength(0)
  })

  it('模式为 observe/enforce 时显示国家选择器', () => {
    const wrapper = mountSection({ ...endpoint, geo_mode: 'observe' })
    expect(wrapper.findAll('.el-select-stub').length).toBeGreaterThan(0)
  })

  it('国家选项包含常见国家（CN/HK/US/JP 等）', () => {
    const wrapper = mountSection({ ...endpoint, geo_mode: 'enforce' })
    const text = wrapper.text()
    expect(text).toContain('中国')
    expect(text).toContain('CN')
    expect(text).toContain('香港')
    expect(text).toContain('HK')
  })

  it('省份区折叠显示警告文案：当前内嵌库无省级数据', () => {
    const wrapper = mountSection({ ...endpoint, geo_mode: 'enforce' })
    const html = wrapper.html()
    expect(html).toContain('省份配置')
    expect(html).toContain('当前内嵌 GeoIP 库为 Country 级')
    expect(html).toContain('请勿在拦截档使用省份配置')
  })

  it('保存按钮调用 updateEndpointGeoConfig 并触发 saved 事件', async () => {
    vi.mocked(endpointsApi.updateEndpointGeoConfig).mockResolvedValue({ success: true })
    const wrapper = mountSection({ ...endpoint, geo_mode: 'observe', geo_countries: 'CN' })
    await flushPromises()

    const saveBtn = wrapper.findAll('button').find((b) => b.text().includes('保存'))
    expect(saveBtn).toBeDefined()
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(endpointsApi.updateEndpointGeoConfig).toHaveBeenCalledWith(1, 'observe', 'CN', '')
    expect(wrapper.emitted('saved')).toBeTruthy()
  })

  it('取消按钮重置表单到端点原值', async () => {
    const wrapper = mountSection({ ...endpoint, geo_mode: 'off', geo_countries: '' })
    await flushPromises()

    // 手动触发模式切换(模拟用户修改)
    const radioGroup = wrapper.find('.el-radio-group-stub')
    await radioGroup.trigger('click')
    await flushPromises()

    // 此时表单值已变,点取消应重置
    const cancelBtn = wrapper.findAll('button').find((b) => b.text().includes('取消'))
    await cancelBtn!.trigger('click')
    await flushPromises()

    // 重置后应回到端点原值 off
    expect(wrapper.find('.el-radio-group-stub').attributes('data-value')).toBe('off')
  })

  it('API 失败时显示错误消息，不触发 saved 事件', async () => {
    vi.mocked(endpointsApi.updateEndpointGeoConfig).mockRejectedValue(new Error('网络错误'))
    const wrapper = mountSection({ ...endpoint, geo_mode: 'observe' })
    await flushPromises()

    const saveBtn = wrapper.findAll('button').find((b) => b.text().includes('保存'))
    await saveBtn!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('saved')).toBeFalsy()
  })
})
