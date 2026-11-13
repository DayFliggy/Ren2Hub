import { defineComponent, nextTick, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TextInput from '@/components/common/TextInput.vue'

function mountParent(type: string, initial = '') {
  const value = ref(initial)
  const unrelated = ref(0)
  const warnings: string[] = []
  const wrapper = mount(
    defineComponent({
      components: { TextInput },
      setup: () => ({ value, type, unrelated }),
      template: `
        <label for="test-input">Amount</label>
        <TextInput id="test-input" v-model="value" :type="type" />
        <span>{{ unrelated }}</span>
      `,
    }),
    { global: { config: { warnHandler: (message) => warnings.push(message) } } }
  )
  return { wrapper, value, unrelated, warnings }
}

describe('TextInput string model contract', () => {
  it('keeps native numeric input and empty edits as strings through a real parent v-model', async () => {
    const { wrapper, value, warnings } = mountParent('number', '1')
    try {
      const input = wrapper.get('input')
      await input.setValue('2')
      expect(value.value).toBe('2')
      expect(input.element.value).toBe('2')
      await input.setValue('')
      expect(value.value).toBe('')
      expect(input.element.value).toBe('')
      await input.setValue('3')
      expect(value.value).toBe('3')
      expect(warnings).toEqual([])
    } finally {
      wrapper.unmount()
    }
  })

  it.each(['text', 'number'])(
    'retains decimal precision and trailing zeros for %s without a Number roundtrip',
    async (type) => {
      const { wrapper, value, warnings } = mountParent(type)
      try {
        const raw = '0.123456789012345678900'
        await wrapper.get('input').setValue(raw)
        expect(value.value).toBe(raw)
        expect(wrapper.get('input').element.value).toBe(raw)
        expect(warnings).toEqual([])
      } finally {
        wrapper.unmount()
      }
    }
  )

  it.each(['compositionend', 'change', 'blur'])(
    'defers Chinese composition until %s and preserves an in-progress edit during parent rendering',
    async (finish) => {
      const { wrapper, value, unrelated, warnings } = mountParent('text')
      try {
        const input = wrapper.get('input')
        await input.trigger('compositionstart')
        input.element.value = '你'
        await input.trigger('input', { isComposing: true })
        expect(value.value).toBe('')
        unrelated.value++
        await nextTick()
        expect(input.element.value).toBe('你')
        value.value = 'server update'
        await nextTick()
        expect(input.element.value).toBe('你')
        input.element.value = '你好'
        await input.trigger(finish)
        expect(value.value).toBe('你好')
        await input.trigger('input')
        expect(value.value).toBe('你好')
        expect(warnings).toEqual([])
      } finally {
        wrapper.unmount()
      }
    }
  )
})
