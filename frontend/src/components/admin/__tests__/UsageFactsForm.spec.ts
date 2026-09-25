import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'
import UsageFactsForm from '../UsageFactsForm.vue'
import type { UsageSchema } from '@/api/modelPricing'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
const schema: UsageSchema = {
  version: 1,
  hash: 'fixture',
  fields: {
    seconds: {
      type: 'number',
      unit: 'second',
      source: 'completion_fact',
      required: true,
      minimum: '0',
      maximum: '60',
    },
    resolution: {
      type: 'string',
      enum: ['1080p'],
      source: 'request_fixed',
      required: true,
    },
    audio: { type: 'boolean', source: 'request_fixed', required: true },
  },
}
describe('typed schema facts', () => {
  it('preserves legacy values without emitting and keeps explicit zero and false', async () => {
    const original = {
      resolution: '720p',
      extra: 'kept',
      seconds: '10',
      audio: true,
    }
    const wrapper = mount(UsageFactsForm, {
      props: {
        schema,
        modelValue: original,
        sources: ['request_fixed', 'completion_fact'],
      },
      global: { plugins: [i18n] },
    })
    expect(
      wrapper.get<HTMLSelectElement>('select[aria-label=resolution]').element
        .value
    ).toBe('720p')
    expect(wrapper.text()).toContain('720p (Saved value)')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    await wrapper.get('input[aria-label=seconds]').setValue('0')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual({
      ...original,
      seconds: '0',
    })
    await wrapper.get('select[aria-label=audio]').setValue('false')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual({
      ...original,
      audio: false,
    })
    await wrapper.get('select[aria-label=audio]').setValue('')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual({
      resolution: '720p',
      extra: 'kept',
      seconds: '10',
    })
    expect(original.resolution).toBe('720p')
    wrapper.unmount()
  })
})
