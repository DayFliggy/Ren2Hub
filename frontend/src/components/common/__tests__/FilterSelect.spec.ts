import { nextTick } from 'vue'
import { afterEach, describe, expect, it } from 'vitest'
import { DOMWrapper, enableAutoUnmount, mount } from '@vue/test-utils'

import FilterSelect from '@/components/common/FilterSelect.vue'
import MultiFilterSelect from '@/components/common/MultiFilterSelect.vue'
import i18n from '@/i18n'

// Teleported panels outlive a test unless the wrapper unmounts first.
enableAutoUnmount(afterEach)

// Panels are teleported to <body>, outside the mounted wrapper.
const inBody = (selector: string) =>
  [...document.body.querySelectorAll<HTMLElement>(selector)].map(
    (element) => new DOMWrapper(element)
  )

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

    expect(inBody('[role="listbox"]')[0]!.attributes('data-handdrawn')).toBe(
      'menu'
    )
    await inBody('[role="option"]')[1]!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual(['ready'])
  })

  it('closes an open single-select menu and ignores picks once disabled', async () => {
    const wrapper = mount(FilterSelect, {
      props: { modelValue: 'all', options, label: 'Status' },
      global: { plugins: [i18n] },
    })

    const trigger = wrapper.get('[role="combobox"]')
    await trigger.trigger('click')
    const option = inBody('[role="option"]')[1]!
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

    const menu = inBody('[role="listbox"]')[0]!
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
    const rows = inBody('[role="option"]')
    expect(rows[1]!.text()).toBe('Anthropic3')
    await rows[0]!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([''])
  })
})
