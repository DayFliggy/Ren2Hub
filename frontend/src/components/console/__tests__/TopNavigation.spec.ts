import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import ConsoleTopbar from '@/components/console/ConsoleTopbar.vue'
import TopNavMenu from '@/components/console/TopNavMenu.vue'
import { getVisibleTopNavItems } from '@/constants/navigation/topNav'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAppStore } from '@/stores'
import { useAuthStore } from '@/stores/auth'

const live = () => 'live' as const
const Empty = defineComponent({ template: '<div />' })

async function setup(role: number, capabilities: Record<string, string>) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().persist({ role } as never)
  useAppStore().status = {
    frontend_capabilities: capabilities,
    checkin_enabled: true,
  } as never
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      ['/activity', 'activity'],
      ['/dashboard', 'dashboard'],
      ['/ops', 'ops-monitor'],
      ['/keys', 'keys'],
    ].map(([path, name]) => ({
      path: path!,
      name: name!,
      component: Empty,
      meta: name === 'ops-monitor' ? { topNav: 'ops' as const } : {},
    })),
  })
  await router.push('/ops')
  await router.isReady()
  return { global: { plugins: [router, i18n, pinia] } }
}

beforeEach(async () => {
  await setLocale('zh-CN')
  await loadMessageDomain('console')
})

describe('top navigation', () => {
  it('disables the shared desktop and mobile check-in entry from native status', async () => {
    const options = await setup(100, { admin: 'live', activity: 'live' })
    useAppStore().status.checkin_enabled = false
    const desktop = mount(TopNavMenu, options)
    await flushPromises()
    expect(desktop.findAll('button')[0]!.attributes()).toHaveProperty(
      'disabled'
    )
    for (const surface of ['desktop', 'mobile'] as const) {
      expect(
        getVisibleTopNavItems({
          isAdmin: true,
          isRoot: true,
          surface,
          checkinEnabled: useAppStore().checkinEnabled,
          featureStatus: live,
        }).find((item) => item.name === 'activities')?.disabled
      ).toBe(true)
    }
    useAppStore().status.checkin_enabled = true
    await flushPromises()
    expect(desktop.findAll('button')[0]!.attributes()).not.toHaveProperty(
      'disabled'
    )
    desktop.unmount()
  })
  it('places the root-only ops monitor between dashboard and console', () => {
    const admin = getVisibleTopNavItems({
      isAdmin: true,
      isRoot: true,
      surface: 'desktop',
      featureStatus: live,
    })
    const member = getVisibleTopNavItems({
      isAdmin: false,
      surface: 'desktop',
      featureStatus: live,
    })
    expect(admin.map((item) => item.name)).toEqual([
      'activities',
      'dashboard',
      'ops',
      'console',
      'community',
    ])
    expect(member.map((item) => item.name)).not.toContain('ops')
    expect(
      getVisibleTopNavItems({
        isAdmin: true,
        isRoot: true,
        surface: 'mobile',
        featureStatus: live,
      }).map((item) => item.name)
    ).toEqual(['activities', 'dashboard', 'ops', 'console'])
  })

  it('fails closed when the admin capability is disabled', () => {
    const items = getVisibleTopNavItems({
      isAdmin: true,
      isRoot: true,
      surface: 'desktop',
      featureStatus: (feature) => (feature === 'admin' ? 'disabled' : 'live'),
    })
    expect(items.find((item) => item.name === 'ops')?.disabled).toBe(true)
    expect(items.find((item) => item.name === 'dashboard')?.disabled).toBe(
      false
    )
  })

  it('renders the desktop menu by role and marks the ops route active', async () => {
    const admin = mount(TopNavMenu, await setup(100, { admin: 'live' }))
    await flushPromises()
    const buttons = admin.findAll('button')
    expect(buttons.slice(0, 5).map((button) => button.text())).toEqual([
      '活动',
      '仪表盘',
      '运维监控',
      '控制台',
      '创意社区',
    ])
    expect(admin.text()).not.toContain('炼金室')
    expect(admin.get('[aria-current="page"]').text()).toBe('运维监控')

    const member = mount(TopNavMenu, await setup(1, { admin: 'live' }))
    await flushPromises()
    expect(member.text()).not.toContain('运维监控')
  })

  it('opens configured docs with native links and explains unavailable links', async () => {
    const options = await setup(1, {})
    const app = useAppStore()
    app.status.docs_link = '/docs'
    const view = mount(TopNavMenu, options)
    await view
      .findAll('button')
      .find((button) => button.text() === '创意社区')!
      .trigger('click')
    const link = view.get('a')
    expect(link.attributes('href')).toBe(new URL('/docs', location.origin).href)
    expect(link.attributes('rel')).toBe('noopener noreferrer')
    app.status.HeaderNavModules = '{"docs":false}'
    await flushPromises()
    expect(view.find('a').exists()).toBe(false)
    expect(view.text()).toContain('已关闭')
    app.status.HeaderNavModules = ''
    app.status.docs_link = 'javascript:alert(1)'
    await flushPromises()
    expect(view.text()).toContain('链接不可用')
    view.unmount()
  })

  it('renders the mobile strip from the same definition', async () => {
    const options = await setup(100, { admin: 'live' })
    const topbar = mount(ConsoleTopbar, {
      global: {
        ...options.global,
        stubs: {
          TopNavMenu: true,
          UserMenu: true,
          CommandPaletteModal: true,
          LanguageSelector: true,
          ThemeSwitcher: true,
        },
      },
    })
    await flushPromises()
    const strip = topbar.get('[data-handdrawn="navigation-strip"]')
    expect(strip.findAll('a').map((item) => item.text())).toEqual([
      '活动',
      '仪表盘',
      '运维监控',
      '控制台',
    ])
    expect(strip.text()).not.toContain('炼金室')
    expect(strip.get('[aria-current="page"]').attributes('href')).toBe('/ops')
  })
})
