import { defineComponent, type Component } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'

import { api } from '@/api/console'
import { parsePublicStatus, publicApi } from '@/api/public'
import CommandPaletteModal from '@/components/console/CommandPaletteModal.vue'
import ConsoleNavStrip from '@/components/console/ConsoleNavStrip.vue'
import ConsoleSidebar from '@/components/console/ConsoleSidebar.vue'
import { consoleNavGroups } from '@/constants/navigation/consoleNav'
import { useSystemSettings } from '@/composables/useSystemSettings'
import i18n, { loadMessageDomain } from '@/i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const EmptyRoute = defineComponent({ template: '<div />' })
const originalCSS = globalThis.CSS

afterEach(() => {
  vi.restoreAllMocks()
  globalThis.CSS = originalCSS
})

async function mountNavigation(
  navigation: Component,
  props = {},
  sidebarModules = '{"admin":{"enabled":true,"channel":false}}'
) {
  const pinia = createPinia()
  setActivePinia(pinia)
  // An administrator without ticket:read, so the ticket queue stays idle.
  useAuthStore().persist({
    id: 2,
    username: 'admin',
    display_name: 'Admin',
    email: 'admin@example.com',
    role: 10,
    status: 1,
    quota: 0,
    used_quota: 0,
    request_count: 0,
    permissions: { admin_permissions: { channel: { read: true } } },
  })
  // controller/misc.go publishes the stored option text as is.
  useAppStore().status = parsePublicStatus({
    frontend_capabilities: { admin: 'live', logs: 'live', api_tokens: 'live' },
    SidebarModulesAdmin: sidebarModules,
  })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/dashboard', name: 'dashboard', component: EmptyRoute },
      ...consoleNavGroups
        .flatMap((group) => group.items)
        .filter((item) => item.route)
        .map((item) => ({
          path: `/console/${String(item.route)}`,
          name: item.route,
          component: EmptyRoute,
        })),
    ],
  })
  await router.push('/console/users')
  await router.isReady()
  return mount(navigation, {
    props,
    global: { plugins: [router, i18n, pinia] },
  })
}

describe('SidebarModulesAdmin in the console navigation', () => {
  it.each([
    ['sidebar', ConsoleSidebar, {}],
    ['mobile strip', ConsoleNavStrip, {}],
    ['command palette', CommandPaletteModal, { open: true }],
  ])(
    'updates the mounted %s after navigation settings are saved',
    async (_, navigation, props) => {
      vi.stubGlobal('CSS', { escape: (value: string) => value })
      await loadMessageDomain('console')
      const wrapper = await mountNavigation(navigation, props, '{}')
      const app = useAppStore()
      const settings = useSystemSettings()
      const hasChannel = () =>
        navigation === CommandPaletteModal
          ? [...document.body.querySelectorAll('[role="option"]')].some(
              (option) =>
                option.textContent?.trim() ===
                i18n.global.t('nav.channelManagement')
            )
          : wrapper
              .find('[data-console-nav-item="channel-management"]')
              .exists()
      expect(hasChannel()).toBe(true)
      expect(app.showPricing).toBe(true)
      vi.spyOn(api, 'get').mockResolvedValue([])
      vi.spyOn(api, 'put').mockResolvedValue(null)
      vi.spyOn(publicApi, 'status').mockResolvedValue({
        ...app.status,
        HeaderNavModules: '{"pricing":false}',
        SidebarModulesAdmin: '{"admin":{"channel":false}}',
      })
      expect(await settings.load(true)).toBe(true)

      expect(
        await settings.saveOptions({
          HeaderNavModules: '{"pricing":false}',
          SidebarModulesAdmin: '{"admin":{"channel":false}}',
        })
      ).toBe(true)
      await flushPromises()

      expect(publicApi.status).toHaveBeenCalledOnce()
      expect(app.showPricing).toBe(false)
      expect(hasChannel()).toBe(false)
      wrapper.unmount()
    }
  )
  it.each([
    ['sidebar', ConsoleSidebar],
    ['mobile strip', ConsoleNavStrip],
  ])('hides a module the option turns off in the %s', async (_, navigation) => {
    await loadMessageDomain('console')
    const wrapper = await mountNavigation(navigation)
    const item = (name: string) =>
      wrapper.find(`[data-console-nav-item="${name}"]`).exists()

    expect(item('channel-management')).toBe(false)
    expect(item('user-management')).toBe(true)
    expect(item('keys')).toBe(true)
    wrapper.unmount()
  })

  it('hides the same module in the command palette', async () => {
    vi.stubGlobal('CSS', { escape: (value: string) => value })
    await loadMessageDomain('console')
    const wrapper = await mountNavigation(CommandPaletteModal, { open: true })
    await flushPromises()
    // ConsoleModal teleports the list to the document body.
    const labels = [...document.body.querySelectorAll('[role="option"]')].map(
      (option) => option.textContent?.trim()
    )

    expect(labels).toContain(i18n.global.t('nav.userManagement'))
    expect(labels).not.toContain(i18n.global.t('nav.channelManagement'))
    wrapper.unmount()
  })
})
