import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import AirportRowTestMenu from './AirportRowTestMenu.vue'

// element-plus 在测试环境未全局注册,用具名桩替身(与兄弟组件测试同一模式)
const ElDropdownStub = defineComponent({
  name: 'ElDropdown',
  emits: ['command'],
  setup(_, { slots }) {
    return () => h('div', { class: 'ElDropdown-stub' }, [slots.default?.(), slots.dropdown?.()])
  }
})
const ElDropdownItemStub = defineComponent({
  name: 'ElDropdownItem',
  props: { command: { type: [Boolean, String, Number], default: undefined } },
  setup(_, { slots }) {
    return () => h('div', { class: 'ElDropdownItem-stub' }, slots.default?.())
  }
})
const ElButtonStub = defineComponent({
  name: 'ElButton',
  props: {
    link: { type: Boolean, default: false },
    type: { type: String, default: '' }
  },
  setup(_, { slots }) {
    return () => h('button', { class: 'ElButton-stub' }, slots.default?.())
  }
})
const SimpleSlotStub = (name: string) =>
  defineComponent({
    name,
    setup(_, { slots }) {
      return () => h('div', { class: `${name}-stub` }, slots.default?.())
    }
  })

const mountMenu = () =>
  mount(AirportRowTestMenu, {
    global: {
      stubs: {
        'el-dropdown': ElDropdownStub,
        'el-dropdown-menu': SimpleSlotStub('ElDropdownMenu'),
        'el-dropdown-item': ElDropdownItemStub,
        'el-button': ElButtonStub,
        'el-icon': SimpleSlotStub('ElIcon')
      }
    }
  })

describe('AirportRowTestMenu', () => {
  it('下拉含抽样测试/测全部两项,command 分别上抛 full=false/true(功能不回归,ticket 0046)', async () => {
    const wrapper = mountMenu()

    const items = wrapper.findAllComponents({ name: 'ElDropdownItem' })
    expect(items).toHaveLength(2)
    expect(items[0].props('command')).toBe(false)
    expect(items[1].props('command')).toBe(true)
    expect(items[0].text()).toContain('抽样测试')
    expect(items[1].text()).toContain('测全部')

    const dropdown = wrapper.findComponent({ name: 'ElDropdown' })
    await dropdown.vm.$emit('command', false)
    await dropdown.vm.$emit('command', true)
    expect(wrapper.emitted('test')).toEqual([[false], [true]])
  })

  it('触发按钮为 link type=primary,与行内「详情/刷新」同形态(ticket 0046)', () => {
    const wrapper = mountMenu()

    const trigger = wrapper.findComponent({ name: 'ElButton' })
    expect(trigger.props('link')).toBe(true)
    expect(trigger.props('type')).toBe('primary')
  })
})
