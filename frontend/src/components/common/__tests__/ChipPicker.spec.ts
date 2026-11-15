import { nextTick } from 'vue'
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import ChipPicker from '@/components/common/ChipPicker.vue'
import i18n from '@/i18n'

function tapOutside() {
  document.body.dispatchEvent(
    new PointerEvent('pointerdown', { bubbles: true })
  )
  document.body.dispatchEvent(new MouseEvent('click', { bubbles: true }))
}

describe('ChipPicker', () => {
  it('closes on an outside tap while the input keeps focus and reopens on tap', async () => {
    const wrapper = mount(ChipPicker, {
      props: { options: ['gpt-5', 'claude'], modelValue: [] },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })
    const input = wrapper.get('input')

    await input.trigger('focus')
    expect(Boolean(document.querySelector('[data-handdrawn="menu"]'))).toBe(
      true
    )

    tapOutside()
    await nextTick()
    expect(Boolean(document.querySelector('[data-handdrawn="menu"]'))).toBe(
      false
    )

    await input.trigger('click')
    expect(Boolean(document.querySelector('[data-handdrawn="menu"]'))).toBe(
      true
    )

    wrapper.unmount()
  })
  it('adds a typed value on Enter only when creatable, once and trimmed', async () => {
    const wrapper = mount(ChipPicker, {
      props: { options: ['gpt-5'], modelValue: ['gpt-5'] },
      global: { plugins: [i18n] },
    })
    const input = wrapper.get('input')
    await input.setValue('custom')
    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await wrapper.setProps({ creatable: true })
    await input.setValue('  custom  ')
    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')).toEqual([
      [['gpt-5', 'custom']],
    ])

    await input.setValue('gpt-5')
    await input.trigger('keydown', { key: 'Enter' })
    expect(wrapper.emitted('update:modelValue')).toHaveLength(1)
    expect((input.element as HTMLInputElement).value).toBe('')
    wrapper.unmount()
  })
  it('keeps the chips fixed while disabled', async () => {
    const wrapper = mount(ChipPicker, {
      props: {
        options: ['gpt-5', 'claude'],
        modelValue: ['gpt-5'],
        creatable: true,
        disabled: true,
      },
      global: { plugins: [i18n] },
      attachTo: document.body,
    })
    const input = wrapper.get('input')
    expect(input.attributes('disabled')).toBeDefined()
    await input.trigger('focus')
    expect(document.querySelector('[data-handdrawn="menu"]')).toBeNull()
    await input.setValue('custom')
    await input.trigger('keydown', { key: 'Enter' })
    await wrapper.get('[data-handdrawn="chip"] button').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    wrapper.unmount()
  })
})
