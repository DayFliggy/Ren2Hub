import { defineComponent } from 'vue'
import { describe, expect, it } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'

import ConsoleMobileNavDrawer from '@/components/console/ConsoleMobileNavDrawer.vue'
import { parsePublicStatus } from '@/api/public'
import { getAccessibleConsoleNavGroups } from '@/constants/navigation/consoleNav'
import i18n, { loadMessageDomain } from '@/i18n'
import { useAppStore } from '@/stores/app'

const EmptyRoute = defineComponent({ template: '<div />' })

async function routerAt(path: string) {
  const consoleRoutes = getAccessibleConsoleNavGroups({
    isAdmin: true,
    hasPermission: () => true,
  })
    .flatMap((group) => group.items)
    .filter((item) => item.route)
    .map((item) => ({
      path: `/console/${String(item.route)}`,
      name: item.route,
      component: EmptyRoute,
    }))
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/dashboard', name: 'dashboard', component: EmptyRoute },
      { path: '/activity', name: 'activity', component: EmptyRoute },
      ...consoleRoutes,
    ],
  })
  await router.push(path)
  await router.isReady()
  return router
}

describe('mobile drawer navigation semantics', () => {
  it('keeps the active route semantic and links keyboard-accessible', async () => {
    await loadMessageDomain('console')
    const router = await routerAt('/console/models')
    const pinia = createPinia()
    useAppStore(pinia).status = parsePublicStatus({
      frontend_capabilities: { user_models: 'live', api_tokens: 'live' },
    })
    const wrapper = mount(ConsoleMobileNavDrawer, {
      props: { open: true },
      global: {
        plugins: [router, i18n, pinia],
        stubs: { teleport: true },
      },
    })

    expect(wrapper.get('[role="dialog"]').attributes('aria-modal')).toBe('true')
    expect(wrapper.get('[aria-current="page"]').attributes('href')).toBe(
      '/console/models'
    )
    for (const link of wrapper.findAll('a[data-console-nav-item]'))
      expect(link.attributes('href')).toMatch(/^\//)
    expect(
      wrapper.find('[data-console-nav-item="subscription"]').exists()
    ).toBe(false)
    expect(
      wrapper.find('[data-console-nav-item="plan-management"]').exists()
    ).toBe(false)
    wrapper.unmount()
  })

  it('uses the configured documentation URL and disables unavailable links', async () => {
    await loadMessageDomain('console')
    const router = await routerAt('/console/models')
    const pinia = createPinia()
    const app = useAppStore(pinia)
    app.status = parsePublicStatus({ docs_link: '/documentation' })
    const wrapper = mount(ConsoleMobileNavDrawer, {
      props: { open: true },
      global: { plugins: [router, i18n, pinia], stubs: { teleport: true } },
    })
    const docs = () => wrapper.get('[data-console-nav-item="docs"]')
    expect(docs().element.tagName).toBe('A')
    expect(docs().attributes('href')).toBe(
      new URL('/documentation', location.origin).href
    )
    expect(docs().attributes('rel')).toBe('noopener noreferrer')
    app.status.HeaderNavModules = '{"docs":false}'
    await flushPromises()
    expect(docs().element.tagName).toBe('BUTTON')
    expect(docs().attributes('disabled')).toBeDefined()
    expect(docs().attributes('href')).toBeUndefined()
    wrapper.unmount()
  })
})
