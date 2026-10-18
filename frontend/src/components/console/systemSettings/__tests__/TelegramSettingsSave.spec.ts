import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import SystemSettingsDomainView from '../SystemSettingsDomainView.vue'
import { api } from '@/api/client'
import { useSystemSettings } from '@/composables/useSystemSettings'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn(), success: vi.fn() }),
}))

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

async function render(
  options: { failKey?: string; secretGate?: Promise<void> } = {}
) {
  const stored: Record<string, string> = {
    'telegram.client_id': '',
    'telegram.client_secret': '',
    TelegramOAuthEnabled: 'false',
  }
  const writes: Array<{ key: string; value: string }> = []
  vi.spyOn(api, 'get').mockImplementation(async (path) => {
    expect(path).toBe('/api/option/')
    return Object.entries(stored).map(([key, value]) => ({
      key,
      value,
    })) as never
  })
  vi.spyOn(api, 'put').mockImplementation(async (path, body) => {
    expect(path).toBe('/api/option/')
    const update = body as { key: string; value: string }
    writes.push(update)
    if (update.key === options.failKey)
      throw new Error('Credential save failed')
    if (update.key === 'telegram.client_secret') await options.secretGate
    if (
      update.key === 'TelegramOAuthEnabled' &&
      update.value === 'true' &&
      (!stored['telegram.client_id'] || !stored['telegram.client_secret'])
    )
      throw new Error('Telegram OAuth is not configured')
    stored[update.key] = update.value
    return undefined as never
  })
  await useSystemSettings().load(true)
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/auth/:section?',
        name: 'system-settings-auth',
        component: SystemSettingsDomainView,
        props: { domain: 'auth' },
      },
    ],
  })
  await router.push('/auth/oauth')
  const wrapper = mount(SystemSettingsDomainView, {
    props: { domain: 'auth' },
    global: { plugins: [i18n, router] },
  })
  await flushPromises()
  const submit = async () => {
    await wrapper
      .get('[id="setting-telegram.client_id"]')
      .setValue('telegram-client')
    await wrapper
      .get('[id="setting-telegram.client_secret"]')
      .setValue('test-secret')
    await wrapper
      .get(
        `[role="switch"][aria-label="${i18n.global.t('systemSettings.fields.telegramEnabled')}"]`
      )
      .trigger('click')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save changes')!
      .trigger('click')
    await flushPromises()
  }
  return { wrapper, submit, stored, writes }
}

describe('Telegram settings use the upstream credential-first save order', () => {
  it('waits for both credentials to persist before enabling login', async () => {
    let finishSecret!: () => void
    const secretGate = new Promise<void>((resolve) => {
      finishSecret = resolve
    })
    const { wrapper, submit, stored, writes } = await render({ secretGate })
    await submit()
    expect(writes.map(({ key }) => key)).toEqual([
      'telegram.client_id',
      'telegram.client_secret',
    ])
    expect(stored.TelegramOAuthEnabled).toBe('false')
    finishSecret()
    await flushPromises()
    expect(writes).toEqual([
      { key: 'telegram.client_id', value: 'telegram-client' },
      { key: 'telegram.client_secret', value: 'test-secret' },
      { key: 'TelegramOAuthEnabled', value: 'true' },
    ])
    expect(stored.TelegramOAuthEnabled).toBe('true')
    wrapper.unmount()
  })

  it.each(['telegram.client_id', 'telegram.client_secret'])(
    'does not enable login when saving %s fails',
    async (failKey) => {
      const { wrapper, submit, stored, writes } = await render({ failKey })
      await submit()
      expect(writes.map(({ key }) => key)).toEqual(
        failKey === 'telegram.client_id'
          ? ['telegram.client_id']
          : ['telegram.client_id', 'telegram.client_secret']
      )
      expect(stored.TelegramOAuthEnabled).toBe('false')
      expect(
        wrapper.get<HTMLInputElement>('[id="setting-telegram.client_secret"]')
          .element.value
      ).toBe('test-secret')
      wrapper.unmount()
    }
  )
})
