import { defineComponent, ref } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { afterEach, beforeAll, beforeEach, describe, expect, it } from 'vitest'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import AffiliateAttributionField from '../AffiliateAttributionField.vue'
import {
  getAffiliateAttribution,
  storeAffiliateAttribution,
} from '@/utils/affiliate'

const Host = defineComponent({
  components: { AffiliateAttributionField },
  setup: () => ({ code: ref('') }),
  template: '<AffiliateAttributionField v-model="code" />',
})
beforeAll(async () => {
  await loadMessageDomain('auth')
  await setLocale('en')
})
beforeEach(() => localStorage.clear())
afterEach(() => localStorage.clear())
async function render(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/sign-up', component: Host }],
  })
  await router.push(path)
  const wrapper = mount(Host, { global: { plugins: [router, i18n] } })
  await flushPromises()
  return { wrapper, router }
}

describe('explicit referral attribution choices', () => {
  it('only uses stored attribution when the query is absent', async () => {
    storeAffiliateAttribution('STORED')
    const { wrapper } = await render('/sign-up')
    expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('STORED')
    wrapper.unmount()
    const empty = await render('/sign-up?aff=')
    expect(empty.wrapper.get<HTMLInputElement>('input').element.value).toBe('')
    expect(getAffiliateAttribution()).toBe('')
    empty.wrapper.unmount()
  })
  it('updates storage when edited and removes both query and form value when explicitly cleared', async () => {
    const { wrapper, router } = await render('/sign-up?aff=ORIGINAL')
    expect(
      wrapper.get<HTMLInputElement>('input[type="text"]').element.value
    ).toBe('ORIGINAL')
    expect(
      wrapper.get<HTMLInputElement>('input[type="checkbox"]').element.checked
    ).toBe(false)
    await wrapper.get('input').setValue('REPLACED')
    expect(getAffiliateAttribution()).toBe('REPLACED')
    await wrapper.get('button').trigger('click')
    await flushPromises()
    expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('')
    expect(router.currentRoute.value.query).not.toHaveProperty('aff')
    expect(getAffiliateAttribution()).toBe('')
    wrapper.unmount()
  })
  it('does not revive expired stored attribution', async () => {
    localStorage.setItem(
      'ren2hub.affiliate-attribution.v1',
      JSON.stringify({ code: 'EXPIRED', expiresAt: Date.now() - 1 })
    )
    const { wrapper } = await render('/sign-up')
    expect(wrapper.get<HTMLInputElement>('input').element.value).toBe('')
    expect(getAffiliateAttribution()).toBe('')
    wrapper.unmount()
  })
})
