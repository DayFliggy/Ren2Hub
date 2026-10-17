import { nextTick } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

import RedemptionGenerateModal from '@/components/console/redemption/RedemptionGenerateModal.vue'
import i18n, { loadMessageDomain } from '@/i18n'

beforeAll(async () => {
  await loadMessageDomain('console')
  i18n.global.locale.value = 'zh-CN'
})

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllEnvs()
})

function buttonByText(text: string): HTMLButtonElement {
  const button = [...document.body.querySelectorAll('button')].find(
    (item) => item.textContent?.trim() === text
  )
  if (!(button instanceof HTMLButtonElement)) {
    throw new Error(`Button not found: ${text}`)
  }
  return button
}

describe('RedemptionGenerateModal', () => {
  it('uses the local day for the custom expiry in the UTC+8 early morning', async () => {
    vi.stubEnv('TZ', 'Asia/Shanghai')
    vi.useFakeTimers({ toFake: ['Date'] })
    // 2026-10-12 03:00 in UTC+8, which is still 2026-10-11 in UTC.
    vi.setSystemTime(new Date(Date.UTC(2026, 9, 11, 19)))
    const wrapper = mount(RedemptionGenerateModal, {
      attachTo: document.body,
      global: { plugins: [i18n] },
      props: { open: true, loading: false },
    })
    try {
      const name = document.querySelector<HTMLInputElement>('input')!
      name.value = 'October'
      name.dispatchEvent(new Event('input', { bubbles: true }))
      await nextTick()
      buttonByText(i18n.global.t('redemption.expiryCustom')).click()
      await nextTick()
      const input =
        document.querySelector<HTMLInputElement>('input[type="date"]')!
      expect(input.min).toBe('2026-10-12')

      input.value = '2026-10-12'
      input.dispatchEvent(new Event('input'))
      await nextTick()
      buttonByText(i18n.global.t('redemption.generate')).click()

      const [payload] = wrapper.emitted('submit')![0] as [
        { expired_time: number },
      ]
      // End of 2026-10-12 in UTC+8, still in the future at 03:00 local.
      expect(payload.expired_time).toBe(
        Date.UTC(2026, 9, 12, 15, 59, 59) / 1000
      )
      expect(payload.expired_time).toBeGreaterThan(Date.now() / 1000)
    } finally {
      wrapper.unmount()
    }
  })
})
