import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import MetadataTagInput from '../MetadataTagInput.vue'

describe('metadata tag input', () => {
  it.each(['Enter', ',', 'Backspace'])(
    'preserves tags while an input method handles %s',
    async (key) => {
      const wrapper = mount(MetadataTagInput, {
        props: {
          modelValue: ['existing'],
          label: 'Tags',
          removeLabel: 'Remove',
        },
      })
      const input = wrapper.get('input')
      if (key !== 'Backspace') await input.setValue('输入中')
      const event = new KeyboardEvent('keydown', {
        key,
        isComposing: true,
        bubbles: true,
        cancelable: true,
      })
      input.element.dispatchEvent(event)
      expect(event.defaultPrevented).toBe(false)
      expect(wrapper.emitted('update:modelValue')).toBeUndefined()
      wrapper.unmount()
    }
  )
})
