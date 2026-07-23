import { nextTick } from 'vue'
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import ConsoleModal from '@/components/common/ConsoleModal.vue'

describe('ConsoleModal', () => {
  it('has an accessible name, traps initial focus and restores the trigger', async () => {
    const trigger = document.createElement('button')
    document.body.append(trigger)
    trigger.focus()

    const wrapper = mount(ConsoleModal, {
      attachTo: document.body,
      props: { open: false, title: 'Delete token' },
      slots: { default: '<button class="confirm">Confirm</button>' },
    })

    await wrapper.setProps({ open: true })
    await nextTick()
    const dialog = document.body.querySelector<HTMLElement>('[role="dialog"]')
    expect(dialog?.getAttribute('aria-labelledby')).toBeTruthy()
    expect(document.activeElement?.classList.contains('confirm')).toBe(true)

    await wrapper.setProps({ open: false })
    await nextTick()
    expect(document.activeElement).toBe(trigger)
    wrapper.unmount()
  })
})
