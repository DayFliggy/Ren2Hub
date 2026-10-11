import { defineComponent } from 'vue'
import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'

import ConsoleNavStrip from '@/components/console/ConsoleNavStrip.vue'
import { getAccessibleConsoleNavGroups } from '@/constants/navigation/consoleNav'
import i18n, { loadMessageDomain } from '@/i18n'

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
    routes: consoleRoutes,
  })
  await router.push(path)
  await router.isReady()
  return router
}

describe('hand-drawn mobile navigation', () => {
  it('marks the console strip and keeps the active route semantic', async () => {
    await loadMessageDomain('console')
    const router = await routerAt('/console/models')
    const wrapper = mount(ConsoleNavStrip, {
      global: { plugins: [router, i18n, createPinia()] },
    })

    expect(wrapper.attributes('data-handdrawn')).toBe('navigation-strip')
    expect(wrapper.classes()).toContain('select-none')
    expect(wrapper.get('[aria-current="page"]').text()).toBeTruthy()
    expect(
      wrapper.find('[data-console-nav-item="subscription"]').exists()
    ).toBe(false)
    expect(
      wrapper.find('[data-console-nav-item="plan-management"]').exists()
    ).toBe(false)
  })
})
