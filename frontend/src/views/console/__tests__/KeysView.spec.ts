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
    'makes desktop and mobile route chips clickable for %s with read %s',
    async (routing, read, count) => {
      expect(await mountWithAccess(routing, read)).toHaveLength(count)
      expect(wrapper!.findAll('a[data-route="token-routing"]')).toHaveLength(0)
    }
  )

  it('labels each key by effective routing mode and channel scope and opens the modal focused on routing', async () => {
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
    const form = wrapper!.findComponent(KeyFormModal)
    expect(form.props('open')).toBe(true)
    expect(form.props('editing')?.id).toBe(5)
    expect(form.props('focus')).toBe('routing')
    expect(wrapper!.find('[data-key-created-notice]').exists()).toBe(false)
  })
})

describe('KeysView key detail popover', () => {
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

  const popovers = () =>
    document.body.querySelectorAll<HTMLElement>('[data-key-quota-popover]')
  const tableRow = (index: number) =>
    wrapper!.findAll('tbody tr')[index]!.element as HTMLElement
  const tableRing = (index: number) =>
    tableRow(index).querySelector<HTMLElement>('[data-key-quota-ring]')!

  async function settle() {
    await nextTick()
    await nextTick()
  }

  function point(target: Element, type: string, pointerType = 'mouse') {
    target.dispatchEvent(
      new PointerEvent(type, { bubbles: type !== 'pointerleave', pointerType })
    )
  }

  afterEach(() => {
    vi.useRealTimers()
  })

  it('opens one popover after a short hover and switches rows at once', async () => {
    await mountList()
    vi.useFakeTimers()
    const typeCell = (index: number) =>
      tableRow(index).querySelector('td[headers$="-type"]')!

    point(typeCell(0), 'pointerover')
    vi.advanceTimersByTime(149)
    await settle()
    expect(popovers()).toHaveLength(0)
    vi.advanceTimersByTime(1)
    await settle()
    expect(popovers()).toHaveLength(1)
    expect(popovers()[0]!.textContent).toContain('Token 1')
    expect(tableRing(0).getAttribute('aria-expanded')).toBe('true')
    expect(tableRing(0).getAttribute('aria-controls')).toBe(popovers()[0]!.id)

    point(typeCell(1), 'pointerover')
    await settle()
    expect(popovers()).toHaveLength(1)
    expect(popovers()[0]!.textContent).toContain('Token 2')
    expect(tableRing(0).getAttribute('aria-expanded')).toBe('false')

    point(wrapper!.get('.key-list-table').element, 'pointerleave')
    vi.advanceTimersByTime(119)
    await settle()
    expect(popovers()).toHaveLength(1)
    vi.advanceTimersByTime(1)
    await settle()
    expect(popovers()).toHaveLength(0)
  })

  it('gets out of the way of the actions next to the ring', async () => {
    await mountList()
    vi.useFakeTimers()
    point(tableRow(0).querySelector('td[headers$="-name"]')!, 'pointerover')
    vi.advanceTimersByTime(150)
    await settle()
    expect(popovers()).toHaveLength(1)

    point(
      tableRow(0).querySelector('td[headers$="-actions"] button')!,
      'pointerover'
    )
    await settle()
    expect(popovers()).toHaveLength(0)
  })

  it('leaves cards to expand inline instead of hovering a popover', async () => {
    await mountList()
    vi.useFakeTimers()
    point(wrapper!.findAll('[data-key-mobile-row]')[0]!.element, 'pointerover')
    vi.advanceTimersByTime(500)
    await settle()
    expect(popovers()).toHaveLength(0)
  })

  it('ignores touch hover and toggles from the ring instead', async () => {
    await mountList()
    vi.useFakeTimers()

    point(tableRow(2), 'pointerover', 'touch')
    vi.advanceTimersByTime(500)
    await settle()
    expect(popovers()).toHaveLength(0)

    tableRing(2).click()
    await settle()
    expect(popovers()).toHaveLength(1)
    expect(popovers()[0]!.textContent).toContain('Token 3')
    tableRing(2).click()
    await settle()
    expect(popovers()).toHaveLength(0)
  })

  it('absorbs the tap that dismisses a pinned popover', async () => {
    await mountList()
    tableRing(2).click()
    await settle()
    const panel = popovers()[0]!
    // A tapped-open popover covers buttons; it must take the tap itself.
    expect(panel.dataset.pinned).toBe('true')
    expect(panel.classList.contains('pointer-events-auto')).toBe(true)

    panel.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }))
    await settle()
    expect(popovers()).toHaveLength(1)
    panel.click()
    await settle()
    expect(popovers()).toHaveLength(0)
  })

  it('closes on Escape and leaves focus on the ring', async () => {
    await mountList()
    const ring = tableRing(0)
    ring.focus()
    ring.click()
    await settle()
    expect(popovers()).toHaveLength(1)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await settle()
    expect(popovers()).toHaveLength(0)
    expect(document.activeElement).toBe(ring)
  })

  it('closes a pinned popover once focus leaves its ring', async () => {
    await mountList()
    const ring = tableRing(0)
    ring.focus()
    ring.click()
    await settle()
    expect(popovers()[0]!.dataset.pinned).toBe('true')

    ring.blur()
    await settle()
    expect(popovers()).toHaveLength(0)
  })

  it('adds only the warning to the popover header, not the status again', async () => {
    await mountList([
      { ...backendTokens[0]!, remain_quota: 50, used_quota: 950 },
      { ...backendTokens[1]!, status: 2 },
    ])
    const header = () => popovers()[0]!.querySelector('header')!
    tableRing(0).click()
    await settle()
    expect(header().querySelector('[data-key-quota-note]')!.textContent).toBe(
      '仅剩 5%'
    )

    tableRing(1).click()
    await settle()
    expect(header().querySelector('[data-key-quota-note]')).toBeNull()
    expect(header().textContent).toContain('禁用')
  })

  it('shows the full key detail with long limits shortened', async () => {
    await mountList([
      {
        ...backendTokens[0]!,
        name: 'Detailed key',
        allow_user_quota_query: true,
        model_limits_enabled: true,
        model_limits: 'gpt-a,gpt-b,gpt-c,gpt-d',
        allow_ips: '10.0.0.1\n10.0.0.2\n10.0.0.3',
      },
      { ...backendTokens[1]!, unlimited_quota: true },
    ])
    const detailRows = () =>
      Object.fromEntries(
        [...popovers()[0]!.querySelectorAll('dt')].map((dt) => [
          dt.textContent!.trim(),
          dt.nextElementSibling!.textContent!.trim(),
        ])
      )

    tableRing(0).click()
    await settle()
    expect(popovers()[0]!.querySelector('header')!.textContent).toContain(
      'Detailed key'
    )
    expect(detailRows()).toMatchObject({
      已用占比: '10%',
      查询账户额度: '允许',
      过期时间: '永不过期',
      模型限制: 'gpt-a, gpt-b, gpt-c +1',
      'IP 限制': '10.0.0.1, 10.0.0.2 +1',
    })

    tableRing(1).click()
    await settle()
    expect(detailRows()).toMatchObject({
      可用额度: '无限额度',
      模型限制: '不限制',
    })
    expect(detailRows()).not.toHaveProperty('已用占比')
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
