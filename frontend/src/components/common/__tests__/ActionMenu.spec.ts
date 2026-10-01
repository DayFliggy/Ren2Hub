import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ActionMenu from '../ActionMenu.vue'

describe('action menu availability', () => {
  it('dismisses an open menu when its action becomes unavailable', async () => {
    const wrapper = mount(ActionMenu, {
      props: { label: 'Actions', items: [{ key: 'delete', label: 'Delete' }] },
      attachTo: document.body,
    })
    try {
      await wrapper.get('button').trigger('click')
      const item =
        document.querySelector<HTMLButtonElement>('[role="menuitem"]')!
      expect(item).not.toBeNull()
      await wrapper.setProps({ disabled: true })
      expect(document.querySelector('[role="menu"]')).toBeNull()
      item.click()
      expect(wrapper.emitted('select')).toBeUndefined()
    } finally {
      wrapper.unmount()
    }
  })
})
