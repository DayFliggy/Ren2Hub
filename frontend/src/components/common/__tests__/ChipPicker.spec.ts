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
    expect(wrapper.find('[data-handdrawn="menu"]').exists()).toBe(true)

    tapOutside()
    await nextTick()
    expect(wrapper.find('[data-handdrawn="menu"]').exists()).toBe(false)

    await input.trigger('click')
    expect(wrapper.find('[data-handdrawn="menu"]').exists()).toBe(true)

    wrapper.unmount()
  })
})
