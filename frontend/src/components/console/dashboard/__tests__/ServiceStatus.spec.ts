import { createPinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it } from 'vitest'

import ServiceStatus from '@/components/console/dashboard/ServiceStatus.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  setLocale('en')
})

beforeEach(() => {
  setActivePinia(createPinia())
})

describe('ServiceStatus', () => {
  it('starts unknown instead of claiming that the service is healthy', () => {
    const pinia = createPinia()
    setActivePinia(pinia)
    const wrapper = mount(ServiceStatus, {
      global: { plugins: [pinia, i18n] },
    })

    expect(wrapper.text()).toContain('UNKNOWN')
    expect(wrapper.text()).not.toContain('ONLINE')
    expect(wrapper.text()).not.toContain('99.95%')
    expect(wrapper.find('[data-status-reachable]').attributes()).toMatchObject({
      'data-status-reachable': 'false',
    })
  })
})
