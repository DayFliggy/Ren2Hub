import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'

import AmountInput from '@/components/common/AmountInput.vue'

describe('AmountInput', () => {
  it('rejects non-finite input and applies finite bounds', async () => {
    const wrapper = mount(AmountInput, {
      props: { modelValue: 1, min: -5, max: 10 },
    })
    const input = wrapper.get('input')

    await input.setValue('Infinity')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([null])

    await input.setValue('15')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([10])

    await input.setValue('-3.5')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([-3.5])
  })
})
