import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { describe, expect, it } from 'vitest'
import MetadataEndpointEditor from '../MetadataEndpointEditor.vue'

function editor(modelValue: string) {
  return mount(MetadataEndpointEditor, {
    props: { modelValue },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
      ],
    },
  })
}

describe('metadata inferred endpoint compatibility', () => {
  it('preserves inferred types when metadata changes without endpoint edits', async () => {
    const source = '["openai", "custom-endpoint"]'
    const wrapper = editor(source)
    expect(wrapper.vm.validate()).toBe(source)
    await wrapper.get('[role="radio"][aria-checked="false"]').trigger('click')
    expect(JSON.parse(wrapper.vm.validate())).toEqual([
      'openai',
      'custom-endpoint',
    ])
    wrapper.unmount()
  })

  it('validates edited paths and serializes explicit endpoint overrides', async () => {
    const wrapper = editor('["custom-endpoint"]')
    await wrapper.findAll('input')[1].setValue('//external.invalid/path')
    expect(() => wrapper.vm.validate()).toThrow()
    await wrapper.findAll('input')[1].setValue('/v1/responses')
    expect(JSON.parse(wrapper.vm.validate())).toEqual({
      'custom-endpoint': { path: '/v1/responses', method: 'POST' },
    })
    wrapper.unmount()
  })

  it('reports table edits as unsaved until the parent replaces the value', async () => {
    const source = JSON.stringify({
      openai: { path: '/v1/chat/completions', method: 'POST' },
    })
    const wrapper = editor(source)
    expect(wrapper.vm.isDirty()).toBe(false)
    await wrapper.findAll('input')[1].setValue('/v1/responses')
    expect(wrapper.vm.isDirty()).toBe(true)
    // A loaded template replaces the whole value and resets the baseline.
    await wrapper.setProps({ modelValue: '' })
    expect(wrapper.vm.isDirty()).toBe(false)
    wrapper.unmount()
  })
})
