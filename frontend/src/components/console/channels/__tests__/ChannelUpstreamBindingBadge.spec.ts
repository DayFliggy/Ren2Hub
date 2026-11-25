import { mount } from '@vue/test-utils'
import { beforeAll, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import type { UpstreamBinding } from '@/api/upstreams'
import ChannelUpstreamBindingBadge from '@/components/console/channels/ChannelUpstreamBindingBadge.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const binding: UpstreamBinding = {
  id: 1,
  account_id: 4,
  remote_key_id: 14,
  channel_id: 9,
  channel_created_time: 1799999900,
  stale: false,
  remote_status: 'active',
  ratio_sync_enabled: true,
  ratio_sync_factor: '1.25',
  ratio_sync_error: '',
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

it('opens the bindings tab of the bound upstream site', async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      {
        path: '/upstreams',
        name: 'upstreams',
        component: { template: '<div />' },
      },
    ],
  })
  await router.push('/')
  const wrapper = mount(ChannelUpstreamBindingBadge, {
    props: { binding },
    global: { plugins: [i18n, router] },
  })
  expect(wrapper.get('a').attributes('href')).toBe(
    '/upstreams?account=4&tab=bindings'
  )
  wrapper.unmount()
})
