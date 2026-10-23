import { nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, afterEach, describe, expect, it, vi } from 'vitest'

import KeyFormModal from '@/components/console/keys/KeyFormModal.vue'
import i18n, { loadMessageDomain } from '@/i18n'
import type { TokenSummary } from '@/types/console'
import { api } from '@/api/console'
import { configureQuotaDisplay } from '@/utils/format'

beforeAll(async () => {
  await loadMessageDomain('console')
  i18n.global.locale.value = 'zh-CN'
})

afterEach(() => vi.restoreAllMocks())

function mountForm(editing: TokenSummary | null = null) {
  return mount(KeyFormModal, {
    attachTo: document.body,
    global: { plugins: [i18n] },
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
      } finally {
        wrapper.unmount()
        configureQuotaDisplay({ quota_per_unit: 500_000 })
      }
    }
  )
  it('defaults new tokens to auto and keeps advanced restrictions hidden until expanded', async () => {
    const wrapper = mountForm()
    const typeOptions = [...document.body.querySelectorAll('[role="radio"]')]

    expect(
      document
        .querySelector('[name="token-allow-user-quota-query"]')
        ?.getAttribute('aria-checked')
    ).toBe('true')
    expect(typeOptions).toHaveLength(2)
    expect(typeOptions[0].getAttribute('aria-checked')).toBe('false')
    expect(typeOptions[1].getAttribute('aria-checked')).toBe('true')
    expect(advancedTrigger().getAttribute('aria-expanded')).toBe('false')
    expect(document.querySelector('[name="token-custom-key"]')).toBeNull()
    expect(document.querySelector('[name="token-ip-limits"]')).toBeNull()

    advancedTrigger().click()
    await nextTick()
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

  it('saves native numeric budgets and permission without legacy ledger fields', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockRejectedValue(new Error('lost response'))
    const editing = {
      id: 2,
      name: 'original',
      type: 'auto',
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
