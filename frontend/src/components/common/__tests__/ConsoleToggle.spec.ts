import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import ConsoleToggle from '@/components/common/ConsoleToggle.vue'

describe('ConsoleToggle', () => {
  it('renders unchecked by default with proper a11y attributes', () => {
    const wrapper = mount(ConsoleToggle, {
      props: { label: 'Enable feature' },
    })

    const button = wrapper.get('button[role="switch"]')
    expect(button.attributes('aria-checked')).toBe('false')
    expect(button.attributes('aria-label')).toBe('Enable feature')
    expect(button.attributes('data-state')).toBe('unchecked')
    expect(button.attributes('data-size')).toBe('md')
    expect(button.classes()).toContain('is-unchecked')
    expect(button.classes()).not.toContain('is-checked')

    const thumb = wrapper.get('.console-toggle-thumb')
    expect(thumb.classes()).toContain('thumb-unchecked')
    expect(thumb.attributes('style')).toContain('translateX(0)')
  })

  it('renders checked when modelValue is true and shifts thumb', () => {
    const wrapper = mount(ConsoleToggle, {
      props: { modelValue: true, label: 'Dark mode' },
    })

    const button = wrapper.get('button[role="switch"]')
    expect(button.attributes('aria-checked')).toBe('true')
    expect(button.attributes('data-state')).toBe('checked')
    expect(button.classes()).toContain('is-checked')

    const thumb = wrapper.get('.console-toggle-thumb')
    expect(thumb.classes()).toContain('thumb-checked')
    expect(thumb.attributes('style')).toContain(
      'translateX(calc(1.5rem - 4px))'
    )
  })

  it('toggles value on click and emits update:modelValue', async () => {
    const wrapper = mount(ConsoleToggle, {
      props: { modelValue: false },
    })

    await wrapper.get('button[role="switch"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([true])
  })

  it('supports size="sm" geometry and translation', () => {
    const wrapper = mount(ConsoleToggle, {
      props: { modelValue: true, size: 'sm' },
    })

    const button = wrapper.get('button[role="switch"]')
    expect(button.classes()).toContain('h-5')
    expect(button.classes()).toContain('w-9')
    expect(button.attributes('data-size')).toBe('sm')

    const thumb = wrapper.get('.console-toggle-thumb')
    expect(thumb.attributes('style')).toContain(
      'translateX(calc(1.25rem - 4px))'
    )
  })

  it('prevents toggling when disabled', async () => {
    const wrapper = mount(ConsoleToggle, {
      props: { modelValue: false, disabled: true },
    })

    const button = wrapper.get('button[role="switch"]')
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.attributes('aria-disabled')).toBe('true')
    expect(button.classes()).toContain('is-disabled')

    await button.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('prevents toggling and shows spinner when loading', async () => {
    const wrapper = mount(ConsoleToggle, {
      props: { modelValue: false, loading: true },
    })

    const button = wrapper.get('button[role="switch"]')
    expect(button.attributes('disabled')).toBeDefined()
    expect(button.classes()).toContain('is-loading')
    expect(wrapper.find('svg.animate-spin').exists()).toBe(true)

    await button.trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })
})
