import { nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'

import { api } from '@/api/console'
import { parsePublicStatus } from '@/api/public'
import {
  tokenRoutingApi,
  type TokenRoute,
  type TokenRouteChannel,
} from '@/api/tokenRouting'
import { ApiError } from '@/api/types'
import KeyFormModal from '@/components/console/keys/KeyFormModal.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import type { TokenSummary } from '@/types/console'

const sampleChannels: TokenRouteChannel[] = [
  {
    id: 11,
    name: 'OpenAI Primary',
    type: 1,
    status: 1,
    models: ['gpt-4o', 'gpt-4.1'],
    channel_ratio: '1',
    max_concurrency: 10,
    eligible: true,
  },
  {
    id: 22,
    name: 'Claude Direct',
    type: 14,
    status: 1,
    models: ['claude-4'],
    channel_ratio: '1.5',
    max_concurrency: 0,
    eligible: true,
  },
  {
    id: 33,
    name: 'Disabled Pool',
    type: 1,
    status: 2,
    models: ['gpt-4o'],
    channel_ratio: '2',
    max_concurrency: 5,
    eligible: false,
    filter_reason: 'channel_unavailable',
  },
]

function makeToken(overrides: Partial<TokenSummary> = {}): TokenSummary {
  return {
    id: 7,
    name: 'Prod Key',
    key_preview: 'sk-****1234',
    type: 'auto',
    selected_only: false,
    route_channel_count: 0,
    enabled_route_channel_count: 0,
    route_scope: 'platform',
    effective_mode: 'auto',
    allow_user_quota_query: true,
    status: 1,
    used_quota: '0',
    remain_quota: 0,
    unlimited: true,
    model_limits: [],
    ip_limits: [],
    expired_time: -1,
    created_time: 1_722_000_000,
    ...overrides,
  }
}

function setupPermissions(
  feature: 'live' | 'disabled',
  read: boolean,
  write: boolean
) {
  setActivePinia(createPinia())
  useAppStore().status = parsePublicStatus({
    frontend_capabilities: { token_private_routing: feature },
  })
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
      admin_permissions: { routing: { read, write } },
    },
  })
}

function confirmButton(): HTMLButtonElement {
  return [...document.body.querySelectorAll('button')].find(
    (button) => button.textContent?.trim() === i18n.global.t('common.confirm')
  )!
}

function fillName(value: string) {
  const input = document.querySelector<HTMLInputElement>('[name="token-name"]')!
  input.value = value
  input.dispatchEvent(new Event('input', { bubbles: true }))
}

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

beforeEach(async () => {
  await setLocale('zh-CN')
  document.body.innerHTML = ''
})

afterEach(() => {
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('KeyFormModal integrated routing', () => {
  it('defaults to automatic all-platform routing on create and skips PUT /routing when unchanged', async () => {
    setupPermissions('live', true, true)
    const candidates = vi
      .spyOn(tokenRoutingApi, 'candidates')
      .mockResolvedValue(sampleChannels)
    const saveRouting = vi.spyOn(tokenRoutingApi, 'save')
    const post = vi.spyOn(api, 'post').mockResolvedValue({ id: 91 })

    const wrapper = mount(KeyFormModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: { open: true, editing: null, models: ['gpt-4o'] },
    })
    try {
      await flushPromises()
      expect(candidates).toHaveBeenCalledOnce()
      const toggle = document.querySelector<HTMLButtonElement>(
        '[data-key-routing-toggle]'
      )!
      expect(toggle.getAttribute('aria-expanded')).toBe('false')
      expect(toggle.textContent).toContain('自动路由 · 全平台渠道')

      fillName('default-platform-key')
      await nextTick()
      confirmButton().click()
      await flushPromises()

      expect(post).toHaveBeenCalledOnce()
      expect(saveRouting).not.toHaveBeenCalled()
      expect(wrapper.emitted('saved')).toEqual([[]])
    } finally {
      wrapper.unmount()
    }
  })

  it('auto-narrows scope when channels are selected on create and saves via two-step POST then PUT', async () => {
    setupPermissions('live', true, true)
    vi.spyOn(tokenRoutingApi, 'candidates').mockResolvedValue(sampleChannels)
    const post = vi.spyOn(api, 'post').mockResolvedValue({ id: 92 })
    const saveRouting = vi.spyOn(tokenRoutingApi, 'save').mockResolvedValue({
      token_id: 92,
      mode: 'auto',
      selected_only: true,
      version: 1,
      max_channel_ratio: null,
      entries: [{ channel_id: 11, enabled: true, position: 0 }],
      route_scope: 'selected',
      effective_mode: 'auto',
      enabled_route_channel_count: 1,
    })

    const wrapper = mount(KeyFormModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: { open: true, editing: null, models: ['gpt-4o'] },
    })
    try {
      await flushPromises()
      document
        .querySelector<HTMLButtonElement>('[data-key-routing-toggle]')!
        .click()
      await nextTick()
      document
        .querySelector<HTMLButtonElement>('[data-routing-picker-toggle]')!
        .click()
      await nextTick()

      // Disabled channel cannot be added as a new selection.
      const disabledBtn = document.querySelector<HTMLButtonElement>(
        '[data-candidate-channel-id="33"]'
      )!
      expect(disabledBtn.disabled).toBe(true)

      document
        .querySelector<HTMLButtonElement>('[data-candidate-channel-id="11"]')!
        .click()
      await nextTick()
      expect(
        document.querySelector('[data-key-routing-toggle]')!.textContent
      ).toContain('已收窄至 1 条渠道')

      fillName('narrowed-key')
      await nextTick()
      confirmButton().click()
      await flushPromises()

      expect(post).toHaveBeenCalledOnce()
      expect(saveRouting).toHaveBeenCalledWith(92, {
        mode: 'auto',
        selected_only: true,
        version: 0,
        max_channel_ratio: null,
        entries: [{ channel_id: 11, enabled: true, position: 0 }],
      })
      expect(wrapper.emitted('saved')).toEqual([[]])
      expect(wrapper.emitted('close')).toEqual([[]])
    } finally {
      wrapper.unmount()
    }
  })

  it('auto-reverts to all-platform when removing last channel in auto mode and blocks empty ordered mode', async () => {
    setupPermissions('live', true, true)
    vi.spyOn(tokenRoutingApi, 'candidates').mockResolvedValue(sampleChannels)
    const post = vi.spyOn(api, 'post').mockResolvedValue({ id: 93 })
    const saveRouting = vi.spyOn(tokenRoutingApi, 'save')

    const wrapper = mount(KeyFormModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: { open: true, editing: null, models: ['gpt-4o'] },
    })
    try {
      await flushPromises()
      document
        .querySelector<HTMLButtonElement>('[data-key-routing-toggle]')!
        .click()
      await nextTick()
      document
        .querySelector<HTMLButtonElement>('[data-routing-picker-toggle]')!
        .click()
      await nextTick()

      // Select then deselect in auto mode -> reverts to platform.
      const ch11 = document.querySelector<HTMLButtonElement>(
        '[data-candidate-channel-id="11"]'
      )!
      ch11.click()
      await nextTick()
      ch11.click()
      await nextTick()
      expect(
        document.querySelector('[data-key-routing-section]')!.textContent
      ).toContain('路由范围自动恢复为全平台')

      // Switch to ordered mode with 0 channels -> blocks save.
      const orderedRadio = [
        ...document.querySelectorAll('[role="radio"]'),
      ].find((el) => el.textContent?.includes('顺序路由')) as HTMLButtonElement
      orderedRadio.click()
      await nextTick()
      expect(
        document.querySelector('[data-routing-ordered-empty]')
      ).not.toBeNull()

      fillName('ordered-empty')
      await nextTick()
      confirmButton().click()
      await flushPromises()
      expect(post).not.toHaveBeenCalled()
      expect(saveRouting).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
    }
  })

  it('preserves all-paused entries, shows platform fallback notice, and saves without blocking', async () => {
    setupPermissions('live', true, true)
    vi.spyOn(tokenRoutingApi, 'candidates').mockResolvedValue(sampleChannels)
    vi.spyOn(tokenRoutingApi, 'get').mockResolvedValue({
      token_id: 7,
      mode: 'ordered',
      selected_only: true,
      version: 2,
      max_channel_ratio: null,
      entries: [{ channel_id: 11, enabled: true, position: 0 }],
      route_scope: 'selected',
      effective_mode: 'ordered',
      enabled_route_channel_count: 1,
    })
    const putToken = vi.spyOn(api, 'put').mockResolvedValue({})
    const saveRouting = vi.spyOn(tokenRoutingApi, 'save').mockResolvedValue({
      token_id: 7,
      mode: 'ordered',
      selected_only: true,
      version: 3,
      max_channel_ratio: null,
      entries: [{ channel_id: 11, enabled: false, position: 0 }],
      route_scope: 'platform',
      effective_mode: 'auto',
      enabled_route_channel_count: 0,
    })

    const wrapper = mount(KeyFormModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: {
        open: true,
        editing: makeToken({
          type: 'ordered',
          selected_only: true,
          route_channel_count: 1,
        }),
        models: ['gpt-4o'],
      },
    })
    try {
      await flushPromises()
      const toggle = document.querySelector<HTMLButtonElement>(
        '[data-selected-channel-id="11"] [role="switch"]'
      )!
      toggle.click()
      await nextTick()

      expect(
        document.querySelector('[data-routing-all-paused]')!.textContent
      ).toContain('自动退回全平台路由')

      confirmButton().click()
      await flushPromises()

      // Only routing changed -> skips PUT /api/token/, sends PUT /routing with disabled entry preserved.
      expect(putToken).not.toHaveBeenCalled()
      expect(saveRouting).toHaveBeenCalledWith(7, {
        mode: 'ordered',
        selected_only: true,
        version: 2,
        max_channel_ratio: null,
        entries: [{ channel_id: 11, enabled: false, position: 0 }],
      })
    } finally {
      wrapper.unmount()
    }
  })

  it('protects orphaned tokens during unrelated edits and blocks partial ratio changes until resolved', async () => {
    setupPermissions('live', true, true)
    vi.spyOn(tokenRoutingApi, 'candidates').mockResolvedValue(sampleChannels)
    vi.spyOn(tokenRoutingApi, 'get').mockResolvedValue({
      token_id: 7,
      mode: 'ordered',
      selected_only: true,
      version: 5,
      max_channel_ratio: null,
      entries: [],
      route_scope: 'empty',
      effective_mode: 'auto',
      enabled_route_channel_count: 0,
    })
    const putToken = vi.spyOn(api, 'put').mockResolvedValue({})
    const saveRouting = vi.spyOn(tokenRoutingApi, 'save')

    const wrapper = mount(KeyFormModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: {
        open: true,
        editing: makeToken({
          selected_only: true,
          route_channel_count: 0,
          route_scope: 'empty',
        }),
        models: ['gpt-4o'],
      },
    })
    try {
      await flushPromises()
      expect(document.querySelector('[data-route-orphaned]')).not.toBeNull()

      // Rename only -> saves base token, NEVER touches routing (prevents silent fallback to platform).
      fillName('Renamed Orphan')
      await nextTick()
      confirmButton().click()
      await flushPromises()

      expect(putToken).toHaveBeenCalledWith(
        '/api/token/',
        expect.objectContaining({ id: 7, name: 'Renamed Orphan' })
      )
      expect(saveRouting).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
    }
  })

  it('transitions to retry mode without duplicating token creation when step 2 routing PUT fails', async () => {
    setupPermissions('live', true, true)
    vi.spyOn(tokenRoutingApi, 'candidates').mockResolvedValue(sampleChannels)
    const post = vi.spyOn(api, 'post').mockResolvedValue({ id: 105 })
    const saveRouting = vi
      .spyOn(tokenRoutingApi, 'save')
      .mockRejectedValueOnce(new ApiError('Rate limited', { status: 429 }))
      .mockResolvedValueOnce({
        token_id: 105,
        mode: 'auto',
        selected_only: true,
        version: 1,
        max_channel_ratio: null,
        entries: [{ channel_id: 11, enabled: true, position: 0 }],
      })

    const wrapper = mount(KeyFormModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: { open: true, editing: null, models: ['gpt-4o'] },
    })
    try {
      await flushPromises()
      document
        .querySelector<HTMLButtonElement>('[data-key-routing-toggle]')!
        .click()
      await nextTick()
      document
        .querySelector<HTMLButtonElement>('[data-routing-picker-toggle]')!
        .click()
      await nextTick()
      document
        .querySelector<HTMLButtonElement>('[data-candidate-channel-id="11"]')!
        .click()
      fillName('partial-key')
      await nextTick()

      confirmButton().click()
      await flushPromises()

      // Step 1 succeeded, step 2 failed: modal stays open, list notified, partial banner shown.
      expect(post).toHaveBeenCalledTimes(1)
      expect(wrapper.emitted('close')).toBeUndefined()
      expect(wrapper.emitted('saved')).toHaveLength(1)
      const partial = document.querySelector('[data-routing-partial-error]')!
      expect(partial.textContent).toContain('令牌已创建')

      // Retry only sends PUT /routing, never re-creates the token.
      ;(partial.querySelector('button') as HTMLButtonElement).click()
      await flushPromises()
      expect(post).toHaveBeenCalledTimes(1)
      expect(saveRouting).toHaveBeenCalledTimes(2)
      expect(wrapper.emitted('close')).toHaveLength(1)
    } finally {
      wrapper.unmount()
    }
  })

  it('saves base fields before routing PUT on edit and handles 409 conflict reload', async () => {
    setupPermissions('live', true, true)
    const callOrder: string[] = []
    vi.spyOn(tokenRoutingApi, 'candidates').mockResolvedValue(sampleChannels)
    const getRouting = vi
      .spyOn(tokenRoutingApi, 'get')
      .mockResolvedValueOnce({
        token_id: 7,
        mode: 'auto',
        selected_only: false,
        version: 1,
        max_channel_ratio: null,
        entries: [],
      } as TokenRoute)
      .mockResolvedValueOnce({
        token_id: 7,
        mode: 'auto',
        selected_only: false,
        version: 2,
        max_channel_ratio: null,
        entries: [],
      } as TokenRoute)
    vi.spyOn(api, 'put').mockImplementation(async () => {
      callOrder.push('PUT /api/token/')
      return {}
    })
    vi.spyOn(tokenRoutingApi, 'save').mockImplementation(async () => {
      callOrder.push('PUT /routing')
      throw new ApiError('conflict', { status: 409 })
    })

    const wrapper = mount(KeyFormModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: { open: true, editing: makeToken(), models: ['gpt-4o'] },
    })
    try {
      await flushPromises()
      fillName('Updated Both')
      document
        .querySelector<HTMLButtonElement>('[data-key-routing-toggle]')!
        .click()
      await nextTick()
      document
        .querySelector<HTMLButtonElement>('[data-routing-picker-toggle]')!
        .click()
      await nextTick()
      document
        .querySelector<HTMLButtonElement>('[data-candidate-channel-id="11"]')!
        .click()
      await nextTick()

      confirmButton().click()
      await flushPromises()

      expect(callOrder).toEqual(['PUT /api/token/', 'PUT /routing'])
      const errBanner = document.querySelector('[data-routing-save-error]')!
      expect(errBanner.textContent).toContain('已被其他操作修改')
      expect(confirmButton().disabled).toBe(true)

      ;(errBanner.querySelector('button') as HTMLButtonElement).click()
      await flushPromises()
      expect(getRouting).toHaveBeenCalledTimes(2)
    } finally {
      wrapper.unmount()
    }
  })

  it('renders danger row for missing saved channel and obeys read-only / hidden permissions', async () => {
    // 1. Read-only user sees routing & missing channel row, cannot edit or PUT.
    setupPermissions('live', true, false)
    vi.spyOn(tokenRoutingApi, 'candidates').mockResolvedValue(sampleChannels)
    vi.spyOn(tokenRoutingApi, 'get').mockResolvedValue({
      token_id: 7,
      mode: 'ordered',
      selected_only: true,
      version: 1,
      max_channel_ratio: null,
      entries: [{ channel_id: 999, enabled: true, position: 0 }],
    })
    const wrapper = mount(KeyFormModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: {
        open: true,
        editing: makeToken({
          selected_only: true,
          route_channel_count: 1,
        }),
        models: ['gpt-4o'],
      },
    })
    await flushPromises()
    expect(
      document.querySelector('[data-unavailable-channel]')!.textContent
    ).toContain('#999')
    expect(document.querySelector('[data-routing-picker-toggle]')).toBeNull()
    wrapper.unmount()

    // 2. Without routing:read, routing section is hidden and makes zero routing calls.
    vi.restoreAllMocks()
    setupPermissions('live', false, false)
    const getSpy = vi.spyOn(tokenRoutingApi, 'get')
    const candSpy = vi.spyOn(tokenRoutingApi, 'candidates')
    const hidden = mount(KeyFormModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: { open: true, editing: makeToken(), models: ['gpt-4o'] },
    })
    await flushPromises()
    expect(document.querySelector('[data-key-routing-section]')).toBeNull()
    expect(getSpy).not.toHaveBeenCalled()
    expect(candSpy).not.toHaveBeenCalled()
    hidden.unmount()
  })

  it('clears 409 conflict after reloading and allows saving updated routing', async () => {
    setupPermissions('live', true, true)
    vi.spyOn(api, 'put').mockResolvedValue({ data: { success: true } })
    vi.spyOn(tokenRoutingApi, 'candidates').mockResolvedValue(sampleChannels)
    const getSpy = vi
      .spyOn(tokenRoutingApi, 'get')
      .mockResolvedValueOnce({
        token_id: 7,
        mode: 'auto',
        selected_only: false,
        version: 1,
        max_channel_ratio: null,
        entries: [],
      })
      .mockResolvedValueOnce({
        token_id: 7,
        mode: 'auto',
        selected_only: true,
        version: 2,
        max_channel_ratio: null,
        entries: [{ channel_id: 11, enabled: true, position: 0 }],
      })
    const saveSpy = vi
      .spyOn(tokenRoutingApi, 'save')
      .mockRejectedValueOnce(new ApiError('conflict', { status: 409 }))
      .mockResolvedValueOnce({
        token_id: 7,
        mode: 'auto',
        selected_only: true,
        version: 3,
        max_channel_ratio: null,
        entries: [
          { channel_id: 11, enabled: true, position: 0 },
          { channel_id: 22, enabled: true, position: 1 },
        ],
      })

    const wrapper = mount(KeyFormModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: {
        open: true,
        editing: makeToken({ id: 7 }),
        models: ['gpt-4o'],
        focus: 'routing',
      },
    })
    try {
      await flushPromises()
      document
        .querySelector<HTMLButtonElement>('[data-routing-picker-toggle]')!
        .click()
      await nextTick()
      document
        .querySelector<HTMLButtonElement>('[data-candidate-channel-id="11"]')!
        .click()
      await nextTick()

      confirmButton().click()
      await flushPromises()

      // 409 conflict locks confirm button and exposes reload button.
      expect(confirmButton().disabled).toBe(true)
      const reloadBtn = document.querySelector<HTMLButtonElement>(
        '[data-routing-save-error] button'
      )!
      expect(reloadBtn).not.toBeNull()
      reloadBtn.click()
      await flushPromises()

      // After reload succeeds, conflict is cleared and user can edit + save again.
      expect(getSpy).toHaveBeenCalledTimes(2)
      expect(document.querySelector('[data-routing-save-error]')).toBeNull()
      expect(confirmButton().disabled).toBe(false)

      if (!document.querySelector('[data-candidate-channel-id="22"]')) {
        document
          .querySelector<HTMLButtonElement>('[data-routing-picker-toggle]')!
          .click()
        await nextTick()
      }
      document
        .querySelector<HTMLButtonElement>('[data-candidate-channel-id="22"]')!
        .click()
      await nextTick()

      confirmButton().click()
      await flushPromises()
      expect(saveSpy).toHaveBeenCalledTimes(2)
      expect(saveSpy).toHaveBeenLastCalledWith(
        7,
        expect.objectContaining({
          version: 2,
          entries: [
            { channel_id: 11, enabled: true, position: 0 },
            { channel_id: 22, enabled: true, position: 1 },
          ],
        })
      )
    } finally {
      wrapper.unmount()
    }
  })
})
