import { beforeAll, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import SystemSettingsField from '../SystemSettingsField.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

describe('payment setting field drafts', () => {
  it('marks incomplete and duplicate group keys invalid without replacing the last valid model', async () => {
    const wrapper = mount(SystemSettingsField, {
      props: {
        field: {
          key: 'TopupGroupRatio',
          labelKey: 'systemSettings.fields.groupRatio',
          kind: 'ratio',
          defaultValue: '{}',
        },
        modelValue: '{"default":1}',
      },
      global: { plugins: [i18n] },
    })
    await wrapper.get('button[aria-label="Add item"]').trigger('click')
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([false])
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    const keys = wrapper.findAll('.settings-structured-row input:not([type])')
    await keys[1]!.setValue('default')
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([false])
    expect(wrapper.get('[role="alert"]').text()).toContain('Duplicate')
    await keys[1]!.setValue('vip')
    await wrapper.findAll('input[type="number"]')[1]!.setValue('0.8')
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([true])
    expect(
      JSON.parse(String(wrapper.emitted('update:modelValue')?.at(-1)?.[0]))
    ).toEqual({ default: 1, vip: 0.8 })
    wrapper.unmount()
  })

  it('keeps invalid EPay collection values visible and reports them to the saving form', async () => {
    const wrapper = mount(SystemSettingsField, {
      props: {
        field: {
          key: 'PayMethods',
          labelKey: 'systemSettings.fields.epayMethods',
          kind: 'json',
          defaultValue: '[]',
        },
        modelValue: '[{"name":"Alipay","type":"alipay"}]',
      },
      global: { plugins: [i18n] },
    })
    await wrapper
      .findAll('.collection-field input')[1]!
      .setValue('invalid method')
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([false])
    expect(String(wrapper.emitted('update:modelValue')?.at(-1)?.[0])).toContain(
      'invalid method'
    )
    wrapper.unmount()
  })
})
