import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import { ref, defineComponent } from 'vue'
import ModelCapabilitiesEditor from '../ModelCapabilitiesEditor.vue'
import type { ModelCapabilities } from '@/api/modelManifest'

function setup(initial?: ModelCapabilities) {
  const card = ref<ModelCapabilities | undefined>(initial)
  const wrapper = mount(
    defineComponent({
      components: { ModelCapabilitiesEditor },
      setup: () => ({ card }),
      template: '<ModelCapabilitiesEditor v-model="card" />',
    }),
    {
      global: {
        plugins: [
          createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
        ],
      },
    }
  )
  return { card, wrapper }
}

describe('capability editor', () => {
  it('does not write an absent card until a field is changed', async () => {
    const { card, wrapper } = setup()
    expect(card.value).toBeUndefined()
    await wrapper.get('select[aria-label="Tool calls"]').setValue('false')
    expect(card.value?.tool_call).toBe(false)
    expect(card.value?.input_modalities).toBeNull()
    await wrapper.get('#capability-reasoning_efforts-state').setValue('known')
    expect(card.value?.reasoning_efforts).toEqual([])
    await wrapper.get('input[aria-label="Reasoning efforts"]').setValue('low, ')
    expect(
      wrapper.get('input[aria-label="Reasoning efforts"]').element
    ).toHaveProperty('value', 'low, ')
    await wrapper
      .get('input[aria-label="Reasoning efforts"]')
      .setValue('low, high')
    expect(card.value?.reasoning_efforts).toEqual(['low', 'high'])
    await wrapper.get('#capability-reasoning_efforts-state').setValue('unknown')
    expect(card.value?.reasoning_efforts).toBeNull()
    wrapper.unmount()
  })
  it('clears known facts explicitly without converting unknown limits into zero', async () => {
    const { card, wrapper } = setup()
    await wrapper.get('input[aria-label="Context limit"]').setValue('64000')
    expect(card.value?.context_limit).toBe(64000)
    await wrapper.get('input[aria-label="Context limit"]').setValue('')
    expect(card.value?.context_limit).toBeNull()
    await wrapper.get('select[aria-label="Tool calls"]').setValue('true')
    await wrapper.get('button').trigger('click')
    expect(card.value).toMatchObject({
      tool_call: null,
      context_limit: null,
      input_modalities: null,
    })
    wrapper.unmount()
  })
})
