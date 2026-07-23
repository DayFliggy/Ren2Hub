import { nextTick } from 'vue'
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import DateRangePicker from '@/components/common/DateRangePicker.vue'
import i18n from '@/i18n'

describe('DateRangePicker keyboard model', () => {
  it('uses roving focus and selects dates with the keyboard', async () => {
    const wrapper = mount(DateRangePicker, {
      attachTo: document.body,
      props: { start: '2026-07-15', end: '' },
      global: { plugins: [i18n] },
    })

    await wrapper.get('button[aria-haspopup="dialog"]').trigger('click')
    await nextTick()
    const active = document.activeElement as HTMLButtonElement
    expect(active.dataset.dateKey).toBe('2026-07-15')

    await active.dispatchEvent(
      new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true })
    )
    await nextTick()
    expect((document.activeElement as HTMLElement).dataset.dateKey).toBe(
      '2026-07-16'
    )

    await (document.activeElement as HTMLElement).dispatchEvent(
      new KeyboardEvent('keydown', { key: 'Enter', bubbles: true })
    )
    expect(wrapper.emitted('update:end')?.at(-1)).toEqual(['2026-07-16'])
  })
})
