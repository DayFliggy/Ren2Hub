import { nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import {
  beforeAll,
  beforeEach,
  afterEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'

import { createPinia, setActivePinia } from 'pinia'
import KeyFormModal from '@/components/console/keys/KeyFormModal.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import type { TokenSummary } from '@/types/console'
import { api } from '@/api/console'
import { parsePublicStatus } from '@/api/public'
import { tokenRoutingApi, type TokenRoute } from '@/api/tokenRouting'
import { ApiError } from '@/api/types'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { configureQuotaDisplay } from '@/utils/format'

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('zh-CN')
})

beforeEach(async () => {
  await setLocale('zh-CN')
})

afterEach(() => vi.restoreAllMocks())

function mountForm(
  editing: TokenSummary | null = null,
  pinia?: ReturnType<typeof createPinia>
) {
  return mount(KeyFormModal, {
    attachTo: document.body,
    global: { plugins: pinia ? [i18n, pinia] : [i18n] },
    props: { open: true, editing, models: ['gpt-4o'] },
  })
}

function advancedTrigger(): HTMLButtonElement {
  const label = i18n.global.t('keys.advancedTitle')
  const trigger = [...document.body.querySelectorAll('button')].find((button) =>
    button.textContent?.includes(label)
  )
  if (!(trigger instanceof HTMLButtonElement)) {
    throw new Error('Advanced settings trigger not found')
  }
  return trigger
}

describe('KeyFormModal', () => {
  it.each([
    [1_000_000, 3_000_000, '3.000000'],
    [1.5, 1, '0.666667'],
  ] as const)(
    'preserves an existing token budget with runtime quota unit %s',
    async (perUnit, quota, displayed) => {
      configureQuotaDisplay({ quota_per_unit: perUnit })
      const put = vi.spyOn(api, 'put').mockResolvedValue(null)
      const wrapper = mountForm({
        id: 7,
        name: 'runtime-unit',
        type: 'auto',
        selected_only: false,
        route_channel_count: 0,
        remain_quota: quota,
        used_quota: '0',
        unlimited: false,
        model_limits: [],
        ip_limits: [],
        expired_time: -1,
      } as unknown as TokenSummary)
      try {
        expect(
          document.querySelector<HTMLInputElement>('[name="token-quota"]')
            ?.value
        ).toBe(displayed)
        const button = [...document.body.querySelectorAll('button')].find(
          (item) => item.textContent?.trim() === i18n.global.t('common.confirm')
        )!
        button.click()
        await flushPromises()
        expect(put).toHaveBeenCalledWith(
          '/api/token/',
          expect.objectContaining({ remain_quota: quota })
        )
        expect(wrapper.emitted('saved')).toEqual([[]])
      } finally {
        wrapper.unmount()
        configureQuotaDisplay({ quota_per_unit: 500_000 })
      }
    }
  )
  it('offers no routing choice on creation and keeps advanced restrictions hidden until expanded', async () => {
    const wrapper = mountForm()

    expect(document.body.querySelectorAll('[role="radio"]')).toHaveLength(0)
    expect(advancedTrigger().getAttribute('aria-expanded')).toBe('false')
    expect(document.querySelector('[name="token-custom-key"]')).toBeNull()
    expect(
      document.querySelector('[name="token-allow-user-quota-query"]')
    ).toBeNull()
    expect(document.querySelector('[name="token-ip-limits"]')).toBeNull()

    advancedTrigger().click()
    await nextTick()
    expect(
      document
        .querySelector('[name="token-allow-user-quota-query"]')
        ?.getAttribute('aria-checked')
    ).toBe('true')
    expect(document.querySelector('[name="token-custom-key"]')).toBeNull()
    expect(document.querySelector('[name="token-ip-limits"]')).not.toBeNull()
    wrapper.unmount()
  })

  it('opens advanced settings when an existing token has restrictions', () => {
    const editing: TokenSummary = {
      id: 1,
      name: 'restricted',
      key_preview: 'sk-abc••••wxyz',
      type: 'auto',
      selected_only: false,
      route_channel_count: 0,
      status: 1,
      used_quota: '0',
      remain_quota: 0,
      unlimited: true,
      model_limits: ['gpt-4o'],
      ip_limits: ['127.0.0.1'],
      expired_time: -1,
      created_time: 1,
    }
    const wrapper = mountForm(editing)

    expect(
      document
        .querySelector('[name="token-allow-user-quota-query"]')
        ?.getAttribute('aria-checked')
    ).toBe('false')
    expect(advancedTrigger().getAttribute('aria-expanded')).toBe('true')
    expect(document.querySelector('[name="token-custom-key"]')).toBeNull()
    expect(
      document.querySelector<HTMLTextAreaElement>('[name="token-ip-limits"]')
        ?.value
    ).toBe('127.0.0.1')
    expect(document.querySelector('[name="token-rate-limit"]')).toBeNull()
    wrapper.unmount()
  })

  it('creates a key without a routing type and reports the creation', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue(null)
    const wrapper = mountForm()
    try {
      const name = document.querySelector<HTMLInputElement>(
        '[name="token-name"]'
      )!
      name.value = 'default-balance'
      name.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
      const confirm = [...document.body.querySelectorAll('button')].find(
        (button) =>
          button.textContent?.trim() === i18n.global.t('common.confirm')
      )!
      confirm.click()
      await flushPromises()
      expect(post).toHaveBeenCalledWith(
        '/api/token/',
        expect.objectContaining({ allow_user_quota_query: true })
      )
      expect(post.mock.calls[0]![1]).not.toHaveProperty('type')
      expect(wrapper.emitted('saved')).toEqual([[]])
    } finally {
      wrapper.unmount()
    }
  })

  it('reveals a denied balance permission without model or IP restrictions and resets on reopen', async () => {
    const wrapper = mountForm({
      id: 8,
      name: 'private-balance',
      key_preview: 'sk-****1234',
      type: 'auto',
      selected_only: false,
      route_channel_count: 0,
      status: 1,
      used_quota: '0',
      remain_quota: 0,
      unlimited: true,
      model_limits: [],
      ip_limits: [],
      allow_user_quota_query: false,
      expired_time: -1,
      created_time: 1,
    })
    try {
      expect(advancedTrigger().getAttribute('aria-expanded')).toBe('true')
      expect(advancedTrigger().textContent).toContain(
        i18n.global.t('keys.advancedConfigured', { count: 1 })
      )
      await wrapper.setProps({ open: false })
      await wrapper.setProps({ open: true, editing: null })
      expect(advancedTrigger().getAttribute('aria-expanded')).toBe('false')
      expect(advancedTrigger().textContent).not.toContain(
        i18n.global.t('keys.advancedConfigured', { count: 1 })
      )
      advancedTrigger().click()
      await nextTick()
      expect(
        document
          .querySelector('[name="token-allow-user-quota-query"]')
          ?.getAttribute('aria-checked')
      ).toBe('true')
    } finally {
      wrapper.unmount()
    }
  })

  it('saves native numeric budgets and permission without legacy ledger fields', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockRejectedValue(new Error('lost response'))
    const editing = {
      id: 2,
      name: 'original',
      type: 'auto',
      selected_only: false,
      route_channel_count: 0,
      remain_quota: 1,
      used_quota: '0',
      unlimited: false,
      model_limits: [],
      ip_limits: [],
      expired_time: -1,
    } as unknown as TokenSummary
    const wrapper = mountForm(editing)
    const submit = async () => {
      const button = [...document.body.querySelectorAll('button')].find(
        (item) => item.textContent?.trim() === i18n.global.t('common.confirm')
      )!
      button.click()
      await flushPromises()
    }
    try {
      await flushPromises()
      await submit()
      await submit()
      const first = put.mock.calls[0][1] as Record<string, unknown>
      const repeated = put.mock.calls[1][1] as Record<string, unknown>
      expect(repeated).toEqual(first)
      expect(first).not.toHaveProperty('operation_id')
      expect(first.remain_quota).toBe(1)
      const name = document.querySelector<HTMLInputElement>(
        '[name="token-name"]'
      )!
      name.value = 'renamed'
      name.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
      await submit()
      const changed = put.mock.calls[2][1] as Record<string, unknown>
      expect(changed.name).toBe('renamed')
      expect(changed.allow_user_quota_query).toBe(false)
      expect(advancedTrigger().getAttribute('aria-expanded')).toBe('true')
      const queryToggle = document.querySelector<HTMLButtonElement>(
        '[name="token-allow-user-quota-query"]'
      )!
      queryToggle.click()
      await nextTick()
      await submit()
      const permissionChanged = put.mock.calls[3][1] as Record<string, unknown>
      expect(permissionChanged.allow_user_quota_query).toBe(true)
    } finally {
      wrapper.unmount()
      put.mockRestore()
    }
  })

  it('shows read-only created timestamp and warns when model limits change on a selected-channel key', async () => {
    const wrapper = mountForm({
      id: 9,
      name: 'scoped-key',
      key_preview: 'sk-****9999',
      type: 'ordered',
      selected_only: true,
      route_channel_count: 2,
      enabled_route_channel_count: 2,
      route_scope: 'selected',
      effective_mode: 'ordered',
      allow_user_quota_query: true,
      status: 1,
      used_quota: '0',
      remain_quota: 0,
      unlimited: true,
      model_limits: ['gpt-4o'],
      ip_limits: [],
      expired_time: -1,
      created_time: 1_722_000_000,
    })
    try {
      expect(document.querySelector('[data-key-created-at]')).not.toBeNull()
      expect(
        document.querySelector('[data-model-limits-routing-hint]')
      ).toBeNull()

      // Remove the model limit chip -> triggers the non-blocking routing review hint.
      const removeChipBtn = document.querySelector<HTMLButtonElement>(
        '[aria-label*="gpt-4o"]'
      )
      removeChipBtn?.click()
      await nextTick()
      if (removeChipBtn) {
        expect(
          document.querySelector('[data-model-limits-routing-hint]')
            ?.textContent
        ).toContain('2')
      }
    } finally {
      wrapper.unmount()
    }
  })
})

describe('KeyFormModal expiry', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    vi.unstubAllEnvs()
  })

  function expiringToken(expiredTime: number): TokenSummary {
    return {
      id: 3,
      name: 'expiring',
      type: 'auto',
      selected_only: false,
      route_channel_count: 0,
      remain_quota: 0,
      used_quota: '0',
      unlimited: true,
      model_limits: [],
      ip_limits: [],
      expired_time: expiredTime,
    } as unknown as TokenSummary
  }

  const expiryInput = () =>
    document.querySelector<HTMLInputElement>('[name="token-expiration"]')

  async function submit() {
    const button = [...document.body.querySelectorAll('button')].find(
      (item) => item.textContent?.trim() === i18n.global.t('common.confirm')
    )!
    button.click()
    await flushPromises()
  }

  async function editExpiry(expiredTime: number, change?: string) {
    const put = vi.spyOn(api, 'put').mockResolvedValue({})
    const wrapper = mountForm(expiringToken(expiredTime))
    await flushPromises()
    if (change !== undefined) {
      expiryInput()!.value = change
      expiryInput()!.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
    }
    await submit()
    wrapper.unmount()
    return put.mock.calls.map(([, body]) => body as Record<string, unknown>)
  }

  it('keeps the stored instant when only the name changes', async () => {
    vi.stubEnv('TZ', 'Asia/Shanghai')
    // 2026-10-12 23:00:30 in UTC+8; a date-only field would move it to 00:00.
    const lateEvening = Date.UTC(2026, 9, 12, 15, 0, 30) / 1000
    const put = vi.spyOn(api, 'put').mockResolvedValue({})
    const wrapper = mountForm(expiringToken(lateEvening))
    await flushPromises()
    // jsdom normalizes the value with milliseconds; browsers omit them.
    expect(expiryInput()?.value).toMatch(/^2026-10-12T23:00:30(\.000)?$/)
    expect(expiryInput()?.getAttribute('step')).toBe('1')
    const name = document.querySelector<HTMLInputElement>(
      '[name="token-name"]'
    )!
    name.value = 'renamed'
    name.dispatchEvent(new Event('input', { bubbles: true }))
    await nextTick()
    await submit()
    wrapper.unmount()
    expect(put.mock.calls[0]![1]).toMatchObject({
      name: 'renamed',
      expired_time: lateEvening,
    })
  })

  it('keeps the second occurrence of a repeated daylight-saving hour', async () => {
    vi.stubEnv('TZ', 'America/New_York')
    // 2026-11-01 01:30:00 EST; the same local text also names 01:30 EDT.
    const secondOccurrence = Date.UTC(2026, 10, 1, 6, 30) / 1000
    const [payload] = await editExpiry(secondOccurrence)
    expect(payload!.expired_time).toBe(secondOccurrence)
  })

  it('sends the chosen local time to the second after a change', async () => {
    vi.stubEnv('TZ', 'Asia/Shanghai')
    const stored = Date.UTC(2026, 9, 12, 15, 0, 30) / 1000
    const [payload] = await editExpiry(stored, '2026-10-20T08:15:45')
    expect(payload!.expired_time).toBe(Date.UTC(2026, 9, 20, 0, 15, 45) / 1000)
  })

  it('sends -1 when expiry is turned off', async () => {
    const put = vi.spyOn(api, 'put').mockResolvedValue({})
    const wrapper = mountForm(expiringToken(1_900_000_000))
    await flushPromises()
    const toggle = document.querySelector<HTMLButtonElement>(
      `[role="switch"][aria-label="${i18n.global.t('keys.expireLabel')}"]`
    )!
    toggle.click()
    await nextTick()
    await submit()
    wrapper.unmount()
    expect(put.mock.calls[0]![1]).toMatchObject({ expired_time: -1 })
  })

  it('sends nothing while expiry is on but the time is invalid', async () => {
    vi.stubEnv('TZ', 'Asia/Shanghai')
    const stored = Date.UTC(2026, 9, 12, 15, 0, 30) / 1000
    for (const value of ['', '2026-02-30T10:00:00']) {
      expect(await editExpiry(stored, value)).toEqual([])
    }
  })
})

describe('KeyFormModal routing policy', () => {
  let activePinia: ReturnType<typeof createPinia> | undefined

  function setupPermissions(
    feature: 'live' | 'disabled',
    read: boolean,
    write: boolean
  ) {
    activePinia = createPinia()
    setActivePinia(activePinia)
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

  function confirmButton(): HTMLButtonElement {
    return [...document.body.querySelectorAll('button')].find(
      (item) => item.textContent?.trim() === i18n.global.t('common.confirm')
    )!
  }

  afterEach(() => {
    activePinia = undefined
    setActivePinia(undefined as unknown as ReturnType<typeof createPinia>)
    document.body.innerHTML = ''
    vi.restoreAllMocks()
  })

  it('keeps creation free of routing controls even with full routing permission', async () => {
    setupPermissions('live', true, true)
    const getRouting = vi.spyOn(tokenRoutingApi, 'get')
    const wrapper = mountForm(null, activePinia)
    try {
      await flushPromises()
      expect(getRouting).not.toHaveBeenCalled()
      expect(document.querySelector('[data-key-form-routing]')).toBeNull()
      expect(document.querySelectorAll('[role="radio"]')).toHaveLength(0)
    } finally {
      wrapper.unmount()
    }
  })

  it('edits routing mode and multiplier cap while preserving entries, and skips PUT /routing when unchanged', async () => {
    setupPermissions('live', true, true)
    const putBase = vi.spyOn(api, 'put').mockResolvedValue(null)
    vi.spyOn(tokenRoutingApi, 'get').mockResolvedValue({
      token_id: 7,
      mode: 'ordered',
      selected_only: true,
      version: 4,
      max_channel_ratio: '1',
      entries: [
        { channel_id: 11, enabled: true, position: 0 },
        { channel_id: 22, enabled: false, position: 1 },
      ],
    })
    const saveRouting = vi.spyOn(tokenRoutingApi, 'save').mockResolvedValue({
      token_id: 7,
      mode: 'auto',
      selected_only: true,
      version: 5,
      max_channel_ratio: '2.5',
      entries: [
        { channel_id: 11, enabled: true, position: 0 },
        { channel_id: 22, enabled: false, position: 1 },
      ],
    })

    const wrapper = mountForm(
      makeToken({
        type: 'ordered',
        selected_only: true,
        route_channel_count: 2,
        enabled_route_channel_count: 1,
        route_scope: 'selected',
        effective_mode: 'ordered',
      }),
      activePinia
    )
    try {
      await flushPromises()
      const ratioInput = document.querySelector<HTMLInputElement>(
        '[name="token-max-channel-ratio"]'
      )!
      // Canonical equivalent ratio ("1.0" vs "1") must not trigger PUT /routing.
      ratioInput.value = '1.0'
      ratioInput.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
      confirmButton().click()
      await flushPromises()
      expect(putBase).toHaveBeenCalledTimes(1)
      expect(saveRouting).not.toHaveBeenCalled()

      // Switch mode to auto and change multiplier cap to 2.5.
      const autoRadio = [...document.querySelectorAll('[role="radio"]')].find(
        (el) => el.textContent?.includes('自动路由')
      ) as HTMLButtonElement
      autoRadio.click()
      ratioInput.value = '2.5'
      ratioInput.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
      confirmButton().click()
      await flushPromises()

      expect(saveRouting).toHaveBeenCalledWith(7, {
        mode: 'auto',
        selected_only: true,
        version: 4,
        max_channel_ratio: '2.5',
        entries: [
          { channel_id: 11, enabled: true, position: 0 },
          { channel_id: 22, enabled: false, position: 1 },
        ],
      })
    } finally {
      wrapper.unmount()
    }
  })

  it('blocks empty ordered mode, validates ratio, and jumps to channel picker with ordered intent', async () => {
    setupPermissions('live', true, true)
    const putBase = vi.spyOn(api, 'put').mockResolvedValue(null)
    const saveRouting = vi.spyOn(tokenRoutingApi, 'save')
    vi.spyOn(tokenRoutingApi, 'get').mockResolvedValue({
      token_id: 7,
      mode: 'auto',
      selected_only: false,
      version: 1,
      max_channel_ratio: null,
      entries: [],
    })

    const editing = makeToken()
    const wrapper = mountForm(editing, activePinia)
    try {
      await flushPromises()
      const orderedRadio = [
        ...document.querySelectorAll('[role="radio"]'),
      ].find((el) => el.textContent?.includes('顺序路由')) as HTMLButtonElement
      orderedRadio.click()
      await nextTick()

      expect(
        document.querySelector('[data-routing-ordered-empty]')
      ).not.toBeNull()
      expect(confirmButton().disabled).toBe(true)
      expect(putBase).not.toHaveBeenCalled()
      expect(saveRouting).not.toHaveBeenCalled()

      document
        .querySelector<HTMLButtonElement>('[data-routing-go-select]')!
        .click()
      await nextTick()
      expect(wrapper.emitted('open-routing')).toEqual([
        [{ token: editing, initialMode: 'ordered' }],
      ])

      const ratioInput = document.querySelector<HTMLInputElement>(
        '[name="token-max-channel-ratio"]'
      )!
      ratioInput.value = 'abc'
      ratioInput.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
      expect(ratioInput.getAttribute('aria-invalid')).toBe('true')
      expect(confirmButton().disabled).toBe(true)
    } finally {
      wrapper.unmount()
    }
  })

  it('handles 409 routing conflict with reload and avoids repeating PUT /api/token/ on retry', async () => {
    setupPermissions('live', true, true)
    const putBase = vi.spyOn(api, 'put').mockResolvedValue(null)
    const getRouting = vi
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
        selected_only: false,
        version: 2,
        max_channel_ratio: null,
        entries: [],
      } as TokenRoute)
    const saveRouting = vi
      .spyOn(tokenRoutingApi, 'save')
      .mockRejectedValueOnce(new ApiError('conflict', { status: 409 }))
      .mockResolvedValueOnce({
        token_id: 7,
        mode: 'auto',
        selected_only: false,
        version: 3,
        max_channel_ratio: '2',
        entries: [],
      })

    const wrapper = mountForm(makeToken(), activePinia)
    try {
      await flushPromises()
      const ratioInput = document.querySelector<HTMLInputElement>(
        '[name="token-max-channel-ratio"]'
      )!
      ratioInput.value = '2'
      ratioInput.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()

      confirmButton().click()
      await flushPromises()
      expect(putBase).toHaveBeenCalledTimes(1)
      expect(wrapper.emitted('saved')).toHaveLength(1)
      expect(wrapper.emitted('close')).toBeUndefined()
      const errBanner = document.querySelector('[data-routing-save-error]')!
      expect(errBanner.textContent).toContain('已被其他操作修改')
      expect(confirmButton().disabled).toBe(true)

      ;(errBanner.querySelector('button') as HTMLButtonElement).click()
      await flushPromises()
      expect(getRouting).toHaveBeenCalledTimes(2)

      const reloadedRatioInput = document.querySelector<HTMLInputElement>(
        '[name="token-max-channel-ratio"]'
      )!
      reloadedRatioInput.value = '2'
      reloadedRatioInput.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
      confirmButton().click()
      await flushPromises()
      // Base payload did not change after the partial save, so PUT /api/token/ is not repeated.
      expect(putBase).toHaveBeenCalledTimes(1)
      expect(saveRouting).toHaveBeenCalledTimes(2)
      expect(saveRouting).toHaveBeenLastCalledWith(7, {
        mode: 'auto',
        selected_only: false,
        version: 2,
        max_channel_ratio: '2',
        entries: [],
      })
      expect(wrapper.emitted('close')).toEqual([[]])
    } finally {
      wrapper.unmount()
    }
  })

  it('keeps base fields editable when routing is read-only and hides routing when disabled', async () => {
    setupPermissions('live', true, false)
    const putBase = vi.spyOn(api, 'put').mockResolvedValue(null)
    const saveRouting = vi.spyOn(tokenRoutingApi, 'save')
    vi.spyOn(tokenRoutingApi, 'get').mockResolvedValue({
      token_id: 7,
      mode: 'auto',
      selected_only: false,
      version: 1,
      max_channel_ratio: '1.5',
      entries: [],
    })

    const wrapper = mountForm(makeToken(), activePinia)
    try {
      await flushPromises()
      expect(document.querySelector('[data-routing-readonly]')).not.toBeNull()
      expect(
        document.querySelector<HTMLInputElement>(
          '[name="token-max-channel-ratio"]'
        )?.disabled
      ).toBe(true)
      expect(confirmButton().disabled).toBe(false)

      confirmButton().click()
      await flushPromises()
      expect(putBase).toHaveBeenCalledTimes(1)
      expect(saveRouting).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
    }

    setupPermissions('disabled', true, true)
    const hiddenWrapper = mountForm(makeToken(), activePinia)
    try {
      await flushPromises()
      expect(document.querySelector('[data-key-form-routing]')).toBeNull()
    } finally {
      hiddenWrapper.unmount()
    }
  })
})
