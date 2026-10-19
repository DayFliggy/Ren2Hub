import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, expect, it, vi } from 'vitest'
import { ref } from 'vue'
import AutoRoutePanel from './AutoRoutePanel.vue'
import messages from '@/i18n/locales/en/console'

const state = vi.hoisted(() => ({ value: {} as Record<string, unknown> }))
vi.mock('@/composables/useAutoRoute', () => ({
  useAutoRoute: () => state.value,
}))
beforeEach(() => {
  state.value = {
    loading: ref(false),
    error: ref('Catalog request failed'),
    lastUpdated: ref<Date | null>(null),
    vendorList: ref([]),
    load: vi.fn(),
  }
})
it('distinguishes a failed first load from an empty catalog', () => {
  const wrapper = mount(AutoRoutePanel, {
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: messages } }),
      ],
    },
  })
  expect(wrapper.get('[role="alert"]').text()).toContain(
    'Catalog request failed'
  )
  expect(wrapper.text()).not.toContain(messages.dashboard.autoRoute.emptyTitle)
  expect(wrapper.find('[role="status"]').exists()).toBe(false)
  wrapper.unmount()
})
it('labels retained cards as a stale snapshot after refresh failure', () => {
  state.value.lastUpdated = ref(new Date('2026-10-19T10:00:00Z'))
  state.value.vendorList = ref([
    { groupSlug: 'openai', groupName: 'OpenAI', channels: [], activeCount: 0 },
  ])
  const wrapper = mount(AutoRoutePanel, {
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: messages } }),
      ],
    },
  })
  expect(wrapper.get('[data-route-group]').text()).toContain('OpenAI')
  expect(wrapper.get('[role="status"]').text()).toBe(
    messages.dashboard.autoRoute.stale
  )
  wrapper.unmount()
})
