import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'

import { api } from '@/api/console'
import { parsePublicStatus } from '@/api/public'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { TokenSummary } from '@/types/console'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import KeyFormModal from '@/components/console/keys/KeyFormModal.vue'
import KeysView from '@/views/console/KeysView.vue'

// Platform auto, ordered list, automatic list, and a list whose channels were deleted.
const routes = [
  { type: 'auto', selected_only: false, route_channel_count: 0 },
  { type: 'ordered', selected_only: true, route_channel_count: 3 },
  { type: 'auto', selected_only: true, route_channel_count: 2 },
  { type: 'ordered', selected_only: true, route_channel_count: 0 },
] as const

const tokens: TokenSummary[] = Array.from({ length: 10 }, (_, index) => ({
  id: index + 1,
  name: `Token ${index + 1}`,
  key_preview: `sk-${index + 1}...demo`,
  ...routes[index % routes.length]!,
  status: 1,
  used_quota: '100',
  remain_quota: 900,
  unlimited: false,
  model_limits: [],
  ip_limits: [],
  expired_time: -1,
  created_time: 1_722_000_000,
}))

const backendTokens = tokens.map((token) => ({
  id: token.id,
  name: token.name,
  key: token.key_preview,
  type: token.type,
  selected_only: token.selected_only,
  route_channel_count: token.route_channel_count,
  status: token.status,
  used_quota: Number(token.used_quota),
  remain_quota: token.remain_quota,
  unlimited_quota: token.unlimited,
  model_limits_enabled: false,
  model_limits: '',
  allow_ips: '',
  expired_time: token.expired_time,
  created_time: token.created_time,
}))

let wrapper: VueWrapper | null = null

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

beforeEach(() => {
  setActivePinia(createPinia())
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.restoreAllMocks()
})

describe('KeysView mobile loading contract', () => {
  it('keeps pagination and focus stable with page-sized skeletons', async () => {
    let tokenRequestCount = 0
    const pendingPage = new Promise<never>(() => {})
    vi.spyOn(api, 'get').mockImplementation((path: string) => {
      if (path === '/api/user/models') {
        return Promise.resolve({ models: [] })
      }
      if (path === '/api/token/') {
        tokenRequestCount++
        if (tokenRequestCount > 1) return pendingPage
        return Promise.resolve({
          items: backendTokens,
          total: 25,
          page: 1,
          page_size: 10,
        })
      }
      return Promise.reject(new Error(`Unexpected API request: ${path}`))
    })

    wrapper = mount(KeysView, {
      attachTo: document.body,
      global: {
        plugins: [i18n],
        stubs: { RouterLink: { template: '<a><slot /></a>' } },
      },
    })
    await flushPromises()

    const pagination = wrapper.get('[data-key-mobile-pagination]')
    const nextPage = pagination.get('button[aria-label="下一页"]')
    const nextPageButton = nextPage.element as HTMLButtonElement
    nextPageButton.focus()
    await nextPage.trigger('click')
    await flushPromises()

    expect(wrapper.findAll('[data-key-mobile-skeleton-row]')).toHaveLength(10)
    expect(
      wrapper.get('[data-key-mobile-pagination]').attributes('aria-busy')
    ).toBe('true')
    expect(document.activeElement).toBe(nextPageButton)
  })
})

describe('KeysView token routing entry', () => {
  async function mountWithAccess(routing: string, read: boolean) {
    useAppStore().status = parsePublicStatus({
      frontend_capabilities: { token_private_routing: routing },
    })
    // controller.buildSelfUserData sends authz.Capabilities for every role.
    useAuthStore().persist({
      id: 7,
      username: 'user',
      display_name: 'User',
      email: 'user@example.com',
      role: 1,
      status: 1,
      quota: 100,
      used_quota: 0,
      request_count: 0,
      permissions: {
        admin_permissions: { routing: { read, write: read } },
      },
    })
    vi.spyOn(api, 'get').mockImplementation((path: string) => {
      if (path === '/api/user/models') return Promise.resolve({ models: [] })
      if (path === '/api/token/')
        return Promise.resolve({
          items: backendTokens,
          total: backendTokens.length,
          page: 1,
          page_size: 10,
        })
      return Promise.reject(new Error(`Unexpected API request: ${path}`))
    })
    wrapper = mount(KeysView, {
      global: {
        plugins: [i18n],
        stubs: {
          RouterLink: {
            props: ['to'],
            template: '<a :data-route="to.name"><slot /></a>',
          },
        },
      },
    })
    await flushPromises()
    return wrapper.findAll('a[data-route="token-routing"]')
  }

  it.each([
    ['live', true, backendTokens.length * 2],
    ['live', false, 0],
    ['disabled', true, 0],
  ])(
    'shows desktop and mobile routing links for %s with read %s',
    async (routing, read, count) => {
      expect(await mountWithAccess(routing, read)).toHaveLength(count)
    }
  )

  it('labels each key by routing mode and channel scope', async () => {
    await mountWithAccess('live', true)
    const labels = wrapper!
      .findAll('td[headers$="-type"]')
      .slice(0, 4)
      .map((cell) => cell.text())
    expect(labels).toEqual([
      '自动 · 全平台',
      '顺序 · 3 条渠道',
      '自动 · 2 条渠道',
      expect.stringContaining('所选渠道已不可用'),
    ])
    const unavailable = wrapper!.findAll('td[headers$="-type"]')[3]!
    expect(unavailable.text()).toContain('没有可用渠道')
    expect(unavailable.get('[data-route-warning]').attributes('title')).toBe(
      i18n.global.t('keys.route.unavailableHint')
    )
  })

  it.each([
    ['live', true, true],
    ['disabled', true, false],
  ])(
    'after creating a key with routing %s, the routing hint shown is %s',
    async (routing, read, shown) => {
      await mountWithAccess(routing, read)
      const form = wrapper!.findComponent(KeyFormModal)
      form.vm.$emit('saved', false)
      await flushPromises()
      expect(wrapper!.find('[data-key-created-notice]').exists()).toBe(false)
      form.vm.$emit('saved', true)
      await flushPromises()
      const notice = wrapper!.find('[data-key-created-notice]')
      expect(notice.exists()).toBe(shown)
      if (!shown) return
      expect(notice.text()).toContain('自动路由和全平台渠道')
      await notice.get('button[aria-label="关闭提示"]').trigger('click')
      expect(wrapper!.find('[data-key-created-notice]').exists()).toBe(false)
    }
  )
})
