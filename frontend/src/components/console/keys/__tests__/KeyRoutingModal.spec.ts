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

import { parsePublicStatus } from '@/api/public'
import {
  tokenRoutingApi,
  type TokenRoute,
  type TokenRouteChannel,
} from '@/api/tokenRouting'
import { ApiError } from '@/api/types'
import KeyRoutingModal from '@/components/console/keys/KeyRoutingModal.vue'
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

function saveButton(): HTMLButtonElement {
  return [...document.body.querySelectorAll('button')].find(
    (button) => button.textContent?.trim() === i18n.global.t('common.save')
  )!
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
  vi.useRealTimers()
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('KeyRoutingModal', () => {
  it('auto-narrows on channel selection, disables ineligible channels, and saves clean payload', async () => {
    setupPermissions('live', true, true)
    const candidates = vi
      .spyOn(tokenRoutingApi, 'candidates')
      .mockResolvedValue(sampleChannels)
    vi.spyOn(tokenRoutingApi, 'get').mockResolvedValue({
      token_id: 7,
      mode: 'auto',
      selected_only: false,
      version: 1,
      max_channel_ratio: null,
      entries: [],
      route_scope: 'platform',
      effective_mode: 'auto',
      enabled_route_channel_count: 0,
    })
    const saveRouting = vi.spyOn(tokenRoutingApi, 'save').mockResolvedValue({
      token_id: 7,
      mode: 'auto',
      selected_only: true,
      version: 2,
      max_channel_ratio: null,
      entries: [{ channel_id: 11, enabled: true, position: 0 }],
      route_scope: 'selected',
      effective_mode: 'auto',
      enabled_route_channel_count: 1,
    })

    const wrapper = mount(KeyRoutingModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: {
        open: true,
        token: makeToken({ model_limits: ['gpt-4o'] }),
      },
    })
    try {
      await flushPromises()
      expect(candidates).toHaveBeenCalledWith(
        { model_limits: ['gpt-4o'], max_channel_ratio: undefined },
        expect.any(AbortSignal)
      )
      expect(saveButton().disabled).toBe(true)

      const disabledBtn = document.querySelector<HTMLButtonElement>(
        '[data-candidate-channel-id="33"]'
      )!
      expect(disabledBtn.disabled).toBe(true)

      document
        .querySelector<HTMLButtonElement>('[data-candidate-channel-id="11"]')!
        .click()
      await nextTick()
      expect(saveButton().disabled).toBe(false)

      saveButton().click()
      await flushPromises()
      expect(saveRouting).toHaveBeenCalledWith(7, {
        mode: 'auto',
        selected_only: true,
        version: 1,
        max_channel_ratio: null,
        entries: [{ channel_id: 11, enabled: true, position: 0 }],
      })
      expect(wrapper.emitted('saved')).toEqual([[]])
      expect(wrapper.emitted('close')).toEqual([[]])
    } finally {
      wrapper.unmount()
    }
  })

  it('shows read-only policy summary, preserves max_channel_ratio on save, and emits edit-policy', async () => {
    setupPermissions('live', true, true)
    const candidates = vi
      .spyOn(tokenRoutingApi, 'candidates')
      .mockResolvedValue(sampleChannels)
    vi.spyOn(tokenRoutingApi, 'get').mockResolvedValue({
      token_id: 7,
      mode: 'auto',
      selected_only: true,
      version: 2,
      max_channel_ratio: '1.5',
      entries: [{ channel_id: 11, enabled: true, position: 0 }],
    })
    const saveRouting = vi.spyOn(tokenRoutingApi, 'save').mockResolvedValue({
      token_id: 7,
      mode: 'auto',
      selected_only: false,
      version: 3,
      max_channel_ratio: '1.5',
      entries: [],
    })

    const token = makeToken()
    const wrapper = mount(KeyRoutingModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: { open: true, token },
    })
    try {
      await flushPromises()
      expect(candidates).toHaveBeenCalledWith(
        { model_limits: [], max_channel_ratio: '1.5' },
        expect.any(AbortSignal)
      )
      expect(document.querySelectorAll('[role="radio"]')).toHaveLength(0)
      expect(
        document.querySelector('[name="token-max-channel-ratio"]')
      ).toBeNull()
      const summary = document.querySelector('[data-routing-policy-summary]')!
      expect(summary.textContent).toContain('自动路由')
      expect(summary.textContent).toContain('×1.5')

      document
        .querySelector<HTMLButtonElement>('[data-routing-edit-policy]')!
        .click()
      await nextTick()
      expect(wrapper.emitted('edit-policy')).toEqual([[token]])

      // Remove the only channel in auto mode -> reverts to platform while preserving max_channel_ratio.
      document
        .querySelector<HTMLButtonElement>('[data-candidate-channel-id="11"]')!
        .click()
      await nextTick()
      expect(
        document.querySelector('[data-key-routing-modal]')!.textContent
      ).toContain('路由范围自动恢复为全平台')

      saveButton().click()
      await flushPromises()
      expect(saveRouting).toHaveBeenCalledWith(7, {
        mode: 'auto',
        selected_only: false,
        version: 2,
        max_channel_ratio: '1.5',
        entries: [],
      })
    } finally {
      wrapper.unmount()
    }
  })

  it('supports initialMode=ordered jump from edit modal to pick channels and save ordered mode in one request', async () => {
    setupPermissions('live', true, true)
    vi.spyOn(tokenRoutingApi, 'candidates').mockResolvedValue(sampleChannels)
    vi.spyOn(tokenRoutingApi, 'get').mockResolvedValue({
      token_id: 7,
      mode: 'auto',
      selected_only: false,
      version: 1,
      max_channel_ratio: '2',
      entries: [],
    })
    const saveRouting = vi.spyOn(tokenRoutingApi, 'save').mockResolvedValue({
      token_id: 7,
      mode: 'ordered',
      selected_only: true,
      version: 2,
      max_channel_ratio: '2',
      entries: [{ channel_id: 11, enabled: true, position: 0 }],
    })

    const wrapper = mount(KeyRoutingModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: { open: true, token: makeToken(), initialMode: 'ordered' },
    })
    try {
      await flushPromises()
      expect(
        document.querySelector('[data-routing-ordered-empty]')!.textContent
      ).toContain('已切至顺序路由')
      expect(saveButton().disabled).toBe(true)

      document
        .querySelector<HTMLButtonElement>('[data-candidate-channel-id="11"]')!
        .click()
      await nextTick()
      expect(document.querySelector('[data-routing-ordered-empty]')).toBeNull()
      expect(saveButton().disabled).toBe(false)

      saveButton().click()
      await flushPromises()
      expect(saveRouting).toHaveBeenCalledWith(7, {
        mode: 'ordered',
        selected_only: true,
        version: 1,
        max_channel_ratio: '2',
        entries: [{ channel_id: 11, enabled: true, position: 0 }],
      })
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

    const wrapper = mount(KeyRoutingModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: {
        open: true,
        token: makeToken({
          type: 'ordered',
          selected_only: true,
          route_channel_count: 1,
        }),
      },
    })
    try {
      await flushPromises()
      document
        .querySelector<HTMLButtonElement>(
          '[data-selected-channel-id="11"] [role="switch"]'
        )!
        .click()
      await nextTick()

      expect(
        document.querySelector('[data-routing-all-paused]')!.textContent
      ).toContain('自动退回全平台路由')

      saveButton().click()
      await flushPromises()
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

  it('warns on orphaned selection, allows restoring all-platform, and handles 409 conflict reload', async () => {
    setupPermissions('live', true, true)
    vi.spyOn(tokenRoutingApi, 'candidates').mockResolvedValue(sampleChannels)
    const getRouting = vi
      .spyOn(tokenRoutingApi, 'get')
      .mockResolvedValueOnce({
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
      .mockResolvedValueOnce({
        token_id: 7,
        mode: 'auto',
        selected_only: false,
        version: 6,
        max_channel_ratio: null,
        entries: [],
      } as TokenRoute)
    vi.spyOn(tokenRoutingApi, 'save').mockRejectedValueOnce(
      new ApiError('conflict', { status: 409 })
    )

    const wrapper = mount(KeyRoutingModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: {
        open: true,
        token: makeToken({
          selected_only: true,
          route_channel_count: 0,
          route_scope: 'empty',
        }),
      },
    })
    try {
      await flushPromises()
      const orphanBanner = document.querySelector('[data-route-orphaned]')!
      expect(orphanBanner).not.toBeNull()

      ;(orphanBanner.querySelector('button') as HTMLButtonElement).click()
      await nextTick()
      saveButton().click()
      await flushPromises()

      const errBanner = document.querySelector('[data-routing-save-error]')!
      expect(errBanner.textContent).toContain('已被其他操作修改')
      expect(saveButton().disabled).toBe(true)

      ;(errBanner.querySelector('button') as HTMLButtonElement).click()
      await flushPromises()
      expect(getRouting).toHaveBeenCalledTimes(2)
    } finally {
      wrapper.unmount()
    }
  })

  it('distinguishes model-limited missing channels and enforces read-only footer without write permission', async () => {
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

    const wrapper = mount(KeyRoutingModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: {
        open: true,
        token: makeToken({
          selected_only: true,
          route_channel_count: 1,
          model_limits: ['claude-4'],
        }),
      },
    })
    try {
      await flushPromises()
      expect(
        document.querySelector('[data-unavailable-channel]')!.textContent
      ).toContain('不支持当前模型限制或不可用')
      expect(document.querySelector('[data-routing-readonly]')).not.toBeNull()
      expect(
        document.querySelector('[data-routing-candidate-picker]')
      ).toBeNull()
      const footerButtons = [
        ...document.body.querySelectorAll(
          'footer button, [role="dialog"] button'
        ),
      ]
      expect(
        footerButtons.some(
          (b) => b.textContent?.trim() === i18n.global.t('common.save')
        )
      ).toBe(false)
      expect(
        footerButtons.some(
          (b) => b.textContent?.trim() === i18n.global.t('common.close')
        )
      ).toBe(true)
    } finally {
      wrapper.unmount()
    }
  })
})
