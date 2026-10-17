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

import KeyFormModal from '@/components/console/keys/KeyFormModal.vue'
import i18n, { loadMessageDomain } from '@/i18n'
import type { TokenSummary } from '@/types/console'
import { api } from '@/api/console'

beforeAll(async () => {
  await loadMessageDomain('console')
  i18n.global.locale.value = 'zh-CN'
})

beforeEach(() => {
  vi.spyOn(api, 'get').mockImplementation(async (path) =>
    path === '/api/token/auto-groups'
      ? { groups: ['default'], max_count: 3 }
      : ({ default: { ratio: 1, desc: '' } } as never)
  )
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
      group: 'default',
      auto_groups: [],
      cross_group_retry: false,
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
      group: 'default',
      auto_groups: [],
      cross_group_retry: false,
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

  it('backfills and saves the expiry as the local calendar day', async () => {
    vi.stubEnv('TZ', 'Asia/Shanghai')
    // 2026-10-12 00:00 in UTC+8, which is still 2026-10-11 in UTC.
    const localMidnight = Date.UTC(2026, 9, 11, 16) / 1000
    const put = vi
      .spyOn(api, 'put')
      .mockRejectedValue(new Error('lost response'))
    const editing = {
      id: 3,
      name: 'expiring',
      type: 'auto',
      remain_quota: 0,
      group: 'default',
      auto_groups: [],
      cross_group_retry: false,
      used_quota: '0',
      unlimited: true,
      model_limits: [],
      ip_limits: [],
      expired_time: localMidnight,
    } as unknown as TokenSummary
    const wrapper = mountForm(editing)
    try {
      await flushPromises()
      expect(
        document.querySelector<HTMLInputElement>('[name="token-expiration"]')
          ?.value
      ).toBe('2026-10-12')
      const button = [...document.body.querySelectorAll('button')].find(
        (item) => item.textContent?.trim() === i18n.global.t('common.confirm')
      )!
      button.click()
      await flushPromises()
      const payload = put.mock.calls[0][1] as Record<string, unknown>
      expect(payload.expired_time).toBe(localMidnight)
    } finally {
      wrapper.unmount()
      put.mockRestore()
      vi.unstubAllEnvs()
    }
  })
})
