import { mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it } from 'vitest'
import UsageSchemaReview from '../UsageSchemaReview.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
describe('custom plugin schema review', () => {
  it('requires explicit field origins and applies only on confirmation', async () => {
    const wrapper = mount(UsageSchemaReview, {
      props: {
        manifest: {
          seconds: { type: 'number', unit: 'second' },
          resolution: { enum: ['1080p'] },
        },
      },
      global: { plugins: [i18n] },
    })
    expect(
      wrapper.get('[data-apply-schema-review]').attributes('disabled')
    ).toBeDefined()
    expect(wrapper.emitted('apply')).toBeUndefined()
    await wrapper
      .get('select[aria-label="seconds Fact source"]')
      .setValue('completion_fact')
    await wrapper
      .get('select[aria-label="resolution Fact source"]')
      .setValue('request_fixed')
    await wrapper.get('[data-apply-schema-review]').trigger('click')
    expect(wrapper.emitted('apply')?.[0]?.[0]).toEqual({
      version: 1,
      hash: '',
      fields: {
        seconds: {
          type: 'number',
          unit: 'second',
          source: 'completion_fact',
          required: false,
          integer: false,
        },
        resolution: {
          type: 'string',
          enum: ['1080p'],
          source: 'request_fixed',
          required: false,
        },
      },
    })
    wrapper.unmount()
  })
})
