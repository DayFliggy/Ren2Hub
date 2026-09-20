import { nextTick } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it, vi } from 'vitest'

import KeyFormModal from '@/components/console/keys/KeyFormModal.vue'
import i18n, { loadMessageDomain } from '@/i18n'
import type { TokenSummary } from '@/types/console'
import { api } from '@/api/console'

beforeAll(async () => {
  await loadMessageDomain('console')
  i18n.global.locale.value = 'zh-CN'
})

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
      available_quota: '0',
      reserved_quota: '0',
      quota_version: 0,
      unlimited: true,
      model_limits: ['gpt-4o'],
      ip_limits: ['127.0.0.1'],
      rate_limit: 60,
      max_ratio: 2,
      load_balance: false,
      channels: [],
      expired_time: -1,
      created_time: 1,
    }
    const wrapper = mountForm(editing)

    expect(advancedTrigger().getAttribute('aria-expanded')).toBe('true')
    expect(document.querySelector('[name="token-custom-key"]')).toBeNull()
    expect(
      document.querySelector<HTMLTextAreaElement>('[name="token-ip-limits"]')
        ?.value
    ).toBe('127.0.0.1')
    expect(document.querySelector('[name="token-rate-limit"]')).toBeNull()
    wrapper.unmount()
  })

  it('reuses an uncertain operation only while all submitted fields match', async () => {
    const put = vi
      .spyOn(api, 'put')
      .mockRejectedValue(new Error('lost response'))
    const editing = {
      id: 2,
      name: 'original',
      type: 'auto',
      available_quota: '1',
      reserved_quota: '0',
      used_quota: '0',
      quota_version: 1,
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
      await submit()
      await submit()
      const first = put.mock.calls[0][1] as Record<string, unknown>
      const repeated = put.mock.calls[1][1] as Record<string, unknown>
      expect(repeated.operation_id).toBe(first.operation_id)
      expect(first.available_quota).toBe('1')
      const name = document.querySelector<HTMLInputElement>(
        '[name="token-name"]'
      )!
      name.value = 'renamed'
      name.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
      await submit()
      const changed = put.mock.calls[2][1] as Record<string, unknown>
      expect(changed.name).toBe('renamed')
      expect(changed.operation_id).not.toBe(first.operation_id)
    } finally {
      wrapper.unmount()
      put.mockRestore()
    }
  })
})
