import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TextInput from '../TextInput.vue'

describe('TextInput numeric values', () => {
  it('keeps its string model contract for zero, negative values and clearing', async () => {
    const wrapper = mount(TextInput, {
      props: { type: 'number', modelValue: '' },
    })
    const input = wrapper.get('input')
    await input.setValue('0')
    await input.setValue('-1')
    await input.setValue('')
    expect(wrapper.emitted('update:modelValue')).toEqual([['0'], ['-1'], ['']])
  })
})
