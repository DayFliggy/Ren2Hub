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
import { nextTick } from 'vue'
import { createPinia, setActivePinia } from 'pinia'

import { api } from '@/api/console'
import { parsePublicStatus } from '@/api/public'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { TokenSummary } from '@/types/console'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import KeyFormModal from '@/components/console/keys/KeyFormModal.vue'
import KeyRoutingModal from '@/components/console/keys/KeyRoutingModal.vue'
import KeysView from '@/views/console/KeysView.vue'

// Platform auto, ordered list, automatic list, all-paused fallback, and orphaned empty selection.
const routes = [
  {
    type: 'auto',
    selected_only: false,
    route_channel_count: 0,
    enabled_route_channel_count: 0,
    route_scope: 'platform',
    effective_mode: 'auto',
  },
  {
    type: 'ordered',
    selected_only: true,
    route_channel_count: 3,
    enabled_route_channel_count: 3,
    route_scope: 'selected',
    effective_mode: 'ordered',
  },
  {
    type: 'auto',
    selected_only: true,
    route_channel_count: 2,
    enabled_route_channel_count: 2,
    route_scope: 'selected',
    effective_mode: 'auto',
  },
  {
    type: 'ordered',
    selected_only: true,
    route_channel_count: 2,
    enabled_route_channel_count: 0,
    route_scope: 'platform',
    effective_mode: 'auto',
  },
  {
    type: 'ordered',
    selected_only: true,
    route_channel_count: 0,
    enabled_route_channel_count: 0,
    route_scope: 'empty',
    effective_mode: 'auto',
  },
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
  enabled_route_channel_count: token.enabled_route_channel_count,
  route_scope: token.route_scope,
  effective_mode: token.effective_mode,
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
      },
    })
    await flushPromises()
    return wrapper.findAll('[data-key-route-trigger]')
  }

  it.each([
    ['live', true, backendTokens.length * 2],
    ['live', false, 0],
    ['disabled', true, 0],
  ])(
    'makes desktop and mobile route chips and action buttons available for %s with read %s',
    async (routing, read, count) => {
      expect(await mountWithAccess(routing, read)).toHaveLength(count)
      expect(wrapper!.findAll('[data-key-routing-action]')).toHaveLength(count)
      expect(wrapper!.findAll('a[data-route="token-routing"]')).toHaveLength(0)
    }
  )

  it('labels each key by effective routing mode and channel scope and opens the standalone routing modal from both chip and action button', async () => {
    await mountWithAccess('live', true)
    const labels = wrapper!
      .findAll('td[headers$="-type"]')
      .slice(0, 5)
      .map((cell) => cell.text())
    expect(labels).toEqual([
      '自动 · 全平台',
      '顺序 · 3 条渠道',
      '自动 · 2 条渠道',
      '自动 · 全平台 · 2 条已暂停',
      expect.stringContaining('自选渠道已失效'),
    ])
    const unavailable = wrapper!.findAll('td[headers$="-type"]')[4]!
    expect(unavailable.text()).toContain('没有可用渠道')
    expect(
      unavailable.get('[data-key-route-trigger]').attributes('title')
    ).toBe(i18n.global.t('keys.route.unavailableHint'))

    await unavailable.get('[data-key-route-trigger]').trigger('click')
    const routingModal = wrapper!.findComponent(KeyRoutingModal)
    const formModal = wrapper!.findComponent(KeyFormModal)
    expect(routingModal.props('open')).toBe(true)
    expect(routingModal.props('token')?.id).toBe(5)
    expect(formModal.props('open')).toBe(false)
    expect(wrapper!.find('[data-key-created-notice]').exists()).toBe(false)

    // Switch from routing modal to edit modal via edit-policy, then back with ordered intent.
    routingModal.vm.$emit('edit-policy', tokens[4]!)
    await nextTick()
    await nextTick()
    expect(routingModal.props('open')).toBe(false)
    expect(formModal.props('open')).toBe(true)
    expect(formModal.props('editing')?.id).toBe(5)

    formModal.vm.$emit('open-routing', {
      token: tokens[0]!,
      initialMode: 'ordered',
    })
    await nextTick()
    await nextTick()
    expect(formModal.props('open')).toBe(false)
    expect(routingModal.props('open')).toBe(true)
    expect(routingModal.props('token')?.id).toBe(1)
    expect(routingModal.props('initialMode')).toBe('ordered')

    // Dedicated row action button opens the standalone routing modal directly.
    routingModal.vm.$emit('close')
    await nextTick()
    await wrapper!
      .findAll('td[headers$="-actions"] [data-key-routing-action]')[1]!
      .trigger('click')
    expect(routingModal.props('open')).toBe(true)
    expect(routingModal.props('token')?.id).toBe(2)
    expect(routingModal.props('initialMode')).toBeNull()
  })
})

describe('KeysView inline quota readout and restrictions', () => {
  async function mountList(items: object[] = backendTokens) {
    vi.spyOn(api, 'get').mockImplementation((path: string) => {
      if (path === '/api/user/models') return Promise.resolve({ models: [] })
      if (path === '/api/token/')
        return Promise.resolve({
          items,
          total: items.length,
          page: 1,
          page_size: 10,
        })
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
    return wrapper
  }

  it('shows remaining/used figures and compact restriction markers inline with zero hover popovers', async () => {
    await mountList([
      {
        ...backendTokens[0]!,
        name: 'Detailed key',
        remain_quota: 1_500_000,
        used_quota: 500_000,
        allow_user_quota_query: false,
        model_limits_enabled: true,
        model_limits: 'gpt-a,gpt-b,gpt-c',
        allow_ips: '10.0.0.1\n10.0.0.2',
      },
      { ...backendTokens[1]!, unlimited_quota: true, used_quota: 250_000 },
    ])
    const rows = wrapper!.findAll('tbody tr')

    // Hovering a table row never spawns a floating popover.
    rows[0]!.element.dispatchEvent(
      new PointerEvent('pointerover', { bubbles: true, pointerType: 'mouse' })
    )
    await nextTick()
    expect(document.body.querySelector('[data-key-quota-popover]')).toBeNull()

    // Inline quota readout renders directly in the table cell.
    const quota0 = rows[0]!.get('td[headers$="-quota"]')
    expect(quota0.get('[data-key-remaining]').text()).toBe('$3.000')
    expect(quota0.get('[data-key-used]').text()).toBe('已用 $1.000')
    expect(quota0.find('button').exists()).toBe(false)

    const quota1 = rows[1]!.get('td[headers$="-quota"]')
    expect(quota1.get('[data-key-remaining]').text()).toBe('无限额度')
    expect(quota1.get('[data-key-used]').text()).toBe('已用 $0.500')

    // Restriction summary sits cleanly in the name subline with full title tooltip.
    const name0 = rows[0]!.get('td[headers$="-name"]')
    expect(name0.get('[data-key-restrictions]').text()).toContain('限 3 个模型')
    expect(name0.get('[data-key-restrictions]').text()).toContain('限 IP')
    expect(name0.get('[data-key-restrictions]').text()).toContain('禁查额度')
    expect(name0.find('p').attributes('title')).toContain('gpt-a, gpt-b, gpt-c')
  })

  it('dims blocked desktop rows, aligns reveal label, and uses solid ring for blocked unlimited keys', async () => {
    await mountList([
      { ...backendTokens[0]!, id: 201, status: 2, unlimited_quota: true },
      { ...backendTokens[1]!, id: 202, status: 4, remain_quota: 0 },
      { ...backendTokens[2]!, id: 203, status: 1, expired_time: 1_000_000_000 },
    ])

    const rows = wrapper!.findAll('tbody tr')
    expect(rows[0]!.classes()).toContain('opacity-80')
    expect(rows[1]!.classes()).not.toContain('opacity-80')
    expect(rows[2]!.classes()).toContain('opacity-80')

    // Desktop key reveal button matches mobile label.
    expect(
      rows[0]!
        .find('td[headers$="-key"] button[aria-label="查看完整密钥"]')
        .exists()
    ).toBe(true)

    // Blocked unlimited key draws a solid blocked ring, not an extra-faded dashed ring.
    const blockedCircles = rows[0]!.findAll(
      '[data-ring-state="disabled"] circle'
    )
    expect(blockedCircles.every((c) => c.attributes('opacity') !== '0.7')).toBe(
      true
    )
  })
})
