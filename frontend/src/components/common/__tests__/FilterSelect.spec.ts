import { nextTick } from 'vue'
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import FilterSelect from '@/components/common/FilterSelect.vue'
import MultiFilterSelect from '@/components/common/MultiFilterSelect.vue'
import i18n from '@/i18n'

const options = [
  { value: 'all', label: 'All' },
  { value: 'ready', label: 'Ready' },
]

describe('hand-drawn filter controls', () => {
  it('marks the single-select trigger and menu while preserving selection', async () => {
    const wrapper = mount(FilterSelect, {
      props: { modelValue: 'all', options, label: 'Status' },
      global: { plugins: [i18n] },
    })

    const trigger = wrapper.get('[role="combobox"]')
    expect(trigger.attributes('data-handdrawn')).toBe('control')
    await trigger.trigger('click')
    await nextTick()

    expect(wrapper.get('[role="listbox"]').attributes('data-handdrawn')).toBe(
      'menu'
    )
    await wrapper.findAll('[role="option"]')[1]!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['ready'])
  })

  it('closes an open single-select menu and ignores picks once disabled', async () => {
    const wrapper = mount(FilterSelect, {
      props: { modelValue: 'all', options, label: 'Status' },
      global: { plugins: [i18n] },
    })

    const trigger = wrapper.get('[role="combobox"]')
    await trigger.trigger('click')
    const option = wrapper.findAll('[role="option"]')[1]!
    expect(trigger.attributes('aria-expanded')).toBe('true')

    await wrapper.setProps({ disabled: true })
    expect(trigger.attributes('aria-expanded')).toBe('false')
    await option.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('marks the multi-select trigger and menu without changing its ARIA model', async () => {
    const wrapper = mount(MultiFilterSelect, {
      props: {
        modelValue: ['ready'],
        options,
        label: 'Status',
        placeholder: 'All statuses',
      },
      global: { plugins: [i18n] },
    })

    const trigger = wrapper.get('[role="combobox"]')
    await trigger.trigger('click')
    await nextTick()

    const menu = wrapper.get('[role="listbox"]')
    expect(trigger.attributes('data-handdrawn')).toBe('control')
    expect(menu.attributes('data-handdrawn')).toBe('menu')
    expect(menu.attributes('aria-multiselectable')).toBe('true')
  })

  it('shows a facet chip value only for a real filter and keeps counts in the panel', async () => {
    const wrapper = mount(FilterSelect, {
      props: {
        modelValue: '',
        options: [
          { value: '', label: 'All types' },
          { value: '14', label: 'Anthropic', count: 3 },
        ],
        label: 'Channel type',
        prefixLabel: 'Type',
        variant: 'facet',
      },
      global: { plugins: [i18n] },
    })

    const trigger = wrapper.get('[role="combobox"]')
    expect(trigger.attributes('aria-label')).toBe('Channel type')
    expect(trigger.text()).toBe('Type')
    expect(wrapper.find('[data-facet-value]').exists()).toBe(false)

    await wrapper.setProps({ modelValue: '14' })
    expect(trigger.attributes('data-facet-active')).toBe('true')
    expect(wrapper.get('[data-facet-value]').text()).toBe('Anthropic')
    expect(trigger.text()).not.toContain('3')

    await trigger.trigger('click')
    const rows = wrapper.findAll('[role="option"]')
    expect(rows[1]!.text()).toBe('Anthropic3')
    await rows[0]!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([''])
  })
})
