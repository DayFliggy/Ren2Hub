import { defineComponent, h } from 'vue'
import { createPinia, setActivePinia, type Pinia } from 'pinia'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import { api } from '@/api/client'
import { ApiError } from '@/api/types'
import LanguageSelector from '@/components/common/LanguageSelector.vue'
import {
  accountLocale,
  useAccountLanguageAdoption,
} from '@/composables/useAccountLanguage'
import { useToast } from '@/composables/useToast'
import i18n, { getLocale, LOCALE_STORAGE_KEY, setLocale } from '@/i18n'
import { useAuthStore } from '@/stores/auth'
import type { UserInfo } from '@/types/auth'

let pinia: Pinia

function user(language?: string): UserInfo {
  return {
    id: 7,
    username: 'demo',
    display_name: 'Demo',
    email: 'demo@example.com',
    role: 1,
    status: 1,
    available_quota: '0',
    reserved_quota: '0',
    used_quota: '0',
    quota_version: 0,
    request_count: 0,
    created_at: 1_700_000_000,
    setting: language === undefined ? undefined : JSON.stringify({ language }),
  }
}

async function chooseEnglish(): Promise<void> {
  const wrapper = mount(LanguageSelector, {
    attachTo: document.body,
    global: { plugins: [pinia, i18n] },
  })
  await wrapper.get('button[aria-haspopup="menu"]').trigger('click')
  const english = wrapper
    .findAll('[role="menuitemradio"]')
    .find((option) => option.text().includes('English'))
  await english!.trigger('click')
  await flushPromises()
  wrapper.unmount()
}

function mountAdoption() {
  return mount(
    defineComponent({
      setup() {
        useAccountLanguageAdoption()
        return () => h('div')
      },
    }),
    { global: { plugins: [pinia] } }
  )
}

// The store loads the auth API lazily; load it once so a click settles in one
// flush.
beforeAll(async () => {
  await import('@/api/auth')
})

beforeEach(async () => {
  pinia = createPinia()
  setActivePinia(pinia)
  await setLocale('zh-CN')
  localStorage.removeItem(LOCALE_STORAGE_KEY)
  const toast = useToast()
  for (const item of [...toast.toasts]) toast.dismiss(item.id)
})

describe('account language', () => {
  it('saves a language chosen while signed in to the account', async () => {
    const auth = useAuthStore()
    auth.persist(user('zh-CN'))
    const put = vi.spyOn(api, 'put').mockResolvedValue(undefined)
    vi.spyOn(api, 'get').mockResolvedValue(user('en'))

    await chooseEnglish()

    expect(getLocale()).toBe('en')
    expect(put).toHaveBeenCalledWith('/api/user/self', { language: 'en' })
    expect(auth.user?.setting).toBe(JSON.stringify({ language: 'en' }))
  })

  it('keeps a signed-out choice on this device only', async () => {
    const put = vi.spyOn(api, 'put')

    await chooseEnglish()

    expect(getLocale()).toBe('en')
    expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('en')
    expect(put).not.toHaveBeenCalled()
  })

  it('shows the error when the account rejects the language', async () => {
    useAuthStore().persist(user('zh-CN'))
    vi.spyOn(api, 'put').mockRejectedValue(
      new ApiError('Unsupported language', { status: 400 })
    )

    await chooseEnglish()

    expect(getLocale()).toBe('en')
    expect(useToast().toasts).toContainEqual(
      expect.objectContaining({
        type: 'error',
        message: 'Unsupported language',
      })
    )
  })

  it('adopts the account language on sign-in when this device has no choice', async () => {
    const wrapper = mountAdoption()
    useAuthStore().persist(user('en'))
    await flushPromises()

    expect(getLocale()).toBe('en')
    wrapper.unmount()
  })

  it('keeps the language this device chose on sign-in', async () => {
    localStorage.setItem(LOCALE_STORAGE_KEY, 'zh-CN')
    const wrapper = mountAdoption()
    useAuthStore().persist(user('en'))
    await flushPromises()

    expect(getLocale()).toBe('zh-CN')
    wrapper.unmount()
  })

  it('maps saved account languages onto console locales', () => {
    expect(accountLocale(JSON.stringify({ language: 'zh-TW' }))).toBe('zh-CN')
    expect(accountLocale(JSON.stringify({ language: 'zhCN' }))).toBe('zh-CN')
    expect(accountLocale(JSON.stringify({ language: 'en-US' }))).toBe('en')
    expect(accountLocale(JSON.stringify({ language: 'fr' }))).toBeNull()
    expect(accountLocale(JSON.stringify({}))).toBeNull()
    expect(accountLocale('not json')).toBeNull()
    expect(accountLocale(undefined)).toBeNull()
  })
})
