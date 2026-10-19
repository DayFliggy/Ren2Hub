import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { expect, it } from 'vitest'
import VendorRouteGroup from './VendorRouteGroup.vue'
import messages from '@/i18n/locales/en/console'
import type { CatalogChannel } from '@/api/modelCatalog'

function channel(id: number, limit = 8): CatalogChannel {
  return {
    id,
    name: `Channel ${id}`,
    type: 1,
    status: 1,
    channel_ratio: '0.25',
    max_concurrency: limit,
    in_flight: 2,
    eligible: true,
    filter_reason: '',
    lab: { id: 'openai', name: 'OpenAI', source: 'vendor' },
  }
}
it('keeps historical expandable cards while displaying native capacity, ratio and unknown history', async () => {
  const wrapper = mount(VendorRouteGroup, {
    attachTo: document.body,
    props: {
      groupSlug: 'openai',
      groupName: 'OpenAI',
      activeCount: 4,
      channels: [channel(1), channel(2), channel(3, 0), channel(4)],
    },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: messages } }),
      ],
    },
  })
  expect(wrapper.findAll('[data-route-health-cell]')).toHaveLength(6)
  expect(
    wrapper.get('[data-route-health-timeline]').attributes('aria-label')
  ).toContain('Historical health is unavailable')
  expect(wrapper.findAll('[data-route-channel]')).toHaveLength(0)
  await wrapper.get('button').trigger('click')
  expect(
    wrapper.findAll('[data-route-channel]').filter((row) => row.isVisible())
  ).toHaveLength(3)
  expect(wrapper.get('[data-route-channel]').text()).toContain('2 / 8')
  expect(wrapper.get('[data-route-ratio]').text()).toContain('×0.25')
  expect(wrapper.findAll('[data-route-channel]')[2]!.text()).toContain('2 / ∞')
  expect(
    wrapper.get('[data-route-channel] [role="img"]').attributes('aria-label')
  ).toContain('2')
  await wrapper.get('[data-route-more-toggle]').trigger('click')
  expect(
    wrapper.findAll('[data-route-channel]').filter((row) => row.isVisible())
  ).toHaveLength(4)
  await wrapper.get('[data-route-more-toggle]').trigger('click')
  expect(
    wrapper.findAll('[data-route-channel]').filter((row) => row.isVisible())
  ).toHaveLength(3)
  await wrapper.get('button').trigger('click')
  expect(wrapper.findAll('[data-route-channel]')).toHaveLength(0)
  wrapper.unmount()
})
