import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import SystemSettingsDomainView from '../SystemSettingsDomainView.vue'
import { api } from '@/api/client'
import { ApiError } from '@/api/types'
import { useSystemSettings } from '@/composables/useSystemSettings'
import { getSystemSettingsDomain } from '@/constants/systemSettingsCatalog'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const toast = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }))
vi.mock('@/composables/useToast', () => ({ useToast: () => toast }))

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
const wrappers: VueWrapper[] = []
afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  vi.clearAllMocks()
})

const providers = [
  {
    name: 'GitHub',
    enable: 'GitHubOAuthEnabled',
    fields: { GitHubClientId: 'client', GitHubClientSecret: 'secret' },
  },
  {
    name: 'LinuxDO',
    enable: 'LinuxDOOAuthEnabled',
    fields: { LinuxDOClientId: 'client', LinuxDOClientSecret: 'secret' },
  },
  {
    name: 'Discord',
    enable: 'discord.enabled',
    fields: {
      'discord.client_id': 'client',
      'discord.client_secret': 'secret',
    },
  },
  {
    name: 'Telegram',
    enable: 'TelegramOAuthEnabled',
    fields: {
      'telegram.client_id': 'client',
      'telegram.client_secret': 'secret',
    },
  },
  {
    name: 'WeChat',
    enable: 'WeChatAuthEnabled',
    fields: {
      WeChatServerAddress: 'https://wechat.example',
      WeChatServerToken: 'secret',
    },
  },
  {
    name: 'OIDC',
    enable: 'oidc.enabled',
    fields: {
      'oidc.client_id': 'client',
      'oidc.client_secret': 'secret',
      'oidc.authorization_endpoint': 'https://oidc.example/auth',
      'oidc.token_endpoint': 'https://oidc.example/token',
      'oidc.user_info_endpoint': 'https://oidc.example/user',
    },
  },
  {
    name: 'Turnstile',
    section: 'bot-protection',
    enable: 'TurnstileCheckEnabled',
    fields: { TurnstileSiteKey: 'site', TurnstileSecretKey: 'secret' },
  },
  {
    name: 'email domains',
    section: 'basic-auth',
    enable: 'EmailDomainRestrictionEnabled',
    fields: { EmailDomainWhitelist: 'example.org' },
  },
] as const

interface Update {
  key: string
  value: string
}
async function render(
  options: {
    section?: string
    initial?: Record<string, string>
    beforeWrite?: (
      update: Update,
      stored: Record<string, string>
    ) => Promise<void> | void
  } = {}
) {
  const section = options.section ?? 'oauth'
  const stored: Record<string, string> = {
    ...Object.fromEntries(providers.map(({ enable }) => [enable, 'false'])),
    ...options.initial,
  }
  const writes: Update[] = []
  vi.spyOn(api, 'get').mockImplementation(async (path) => {
    expect(path).toBe('/api/option/')
    return Object.entries(stored).map(([key, value]) => ({
      key,
      value,
    })) as never
  })
  vi.spyOn(api, 'put').mockImplementation(async (path, body) => {
    expect(path).toBe('/api/option/')
    const update = body as Update
    writes.push(update)
    await options.beforeWrite?.(update, stored)
    const provider = providers.find(({ enable }) => enable === update.key)
    if (
      provider &&
      update.value === 'true' &&
      Object.keys(provider.fields).some((key) => !stored[key])
    ) {
      throw new Error(`${provider.name} credentials are not configured`)
    }
    stored[update.key] = update.value
    return undefined as never
  })
  const discovery = vi.spyOn(api, 'post').mockResolvedValue({
    well_known_url: 'https://oidc.example/.well-known/openid-configuration',
    discovery: {
      authorization_endpoint: 'https://oidc.example/auth',
      token_endpoint: 'https://oidc.example/token',
      userinfo_endpoint: 'https://oidc.example/user',
    },
  } as never)
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
  await router.push(`/auth/${section}`)
  const wrapper = mount(SystemSettingsDomainView, {
    props: { domain: 'auth' },
    global: { plugins: [i18n, router] },
  })
  wrappers.push(wrapper)
  await flushPromises()
  const input = (key: string) =>
    wrapper.get<HTMLInputElement>(`[id="setting-${key}"]`)
  const toggle = async (key: string) => {
    const field = getSystemSettingsDomain('auth')!
      .sections.flatMap((item) => item.fields)
      .find((item) => item.key === key)!
    await wrapper
      .get(`[role="switch"][aria-label="${i18n.global.t(field.labelKey)}"]`)
      .trigger('click')
  }
  const save = async () => {
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save changes')!
      .trigger('click')
    await flushPromises()
  }
  return { wrapper, input, toggle, save, writes, stored, discovery }
}

describe('dependent authentication options', () => {
  it.each(providers)(
    'persists $name prerequisites before enabling',
    async (provider) => {
      const view = await render({
        section: 'section' in provider ? provider.section : 'oauth',
      })
      for (const [key, value] of Object.entries(provider.fields))
        await view.input(key).setValue(value)
      await view.toggle(provider.enable)
      await view.save()
      expect(view.writes.map(({ key }) => key)).toEqual([
        ...Object.keys(provider.fields),
        provider.enable,
      ])
      expect(view.stored[provider.enable]).toBe('true')
      expect(view.discovery).not.toHaveBeenCalled()
      expect(toast.error).not.toHaveBeenCalled()
    }
  )

  it.each(providers)(
    'disables $name before clearing a public prerequisite',
    async (provider) => {
      const first = Object.keys(provider.fields)[0]!
      const view = await render({
        section: 'section' in provider ? provider.section : 'oauth',
        initial: { ...provider.fields, [provider.enable]: 'true' },
        beforeWrite(update, stored) {
          if (update.key === first && update.value === '')
            expect(stored[provider.enable]).toBe('false')
        },
      })
      // The upstream options endpoint omits all *Key fields, including the public site key.
      if (first === 'TurnstileSiteKey')
        await view.input(first).setValue('replacement-site')
      else await view.input(first).setValue('')
      await view.toggle(provider.enable)
      await view.save()
      expect(view.writes[0]).toEqual({ key: provider.enable, value: 'false' })
      expect(view.stored[provider.enable]).toBe('false')
      expect(view.discovery).not.toHaveBeenCalled()
    }
  )

  it('keeps only unfinished changes after partial success and does not resubmit confirmed secrets', async () => {
    let fail = true
    const view = await render({
      beforeWrite(update) {
        if (update.key === 'GitHubOAuthEnabled' && fail)
          throw new ApiError('Enable rejected', { business: true })
      },
    })
    await view.input('GitHubClientId').setValue('client')
    await view.input('GitHubClientSecret').setValue('secret')
    await view.toggle('GitHubOAuthEnabled')
    await view.save()
    expect(view.input('GitHubClientSecret').element.value).toBe('')
    expect(view.input('GitHubClientId').element.value).toBe('client')
    expect(toast.error).toHaveBeenLastCalledWith(
      expect.stringContaining('2 settings were saved')
    )
    expect(toast.success).not.toHaveBeenCalled()
    fail = false
    view.writes.length = 0
    await view.save()
    expect(view.writes).toEqual([{ key: 'GitHubOAuthEnabled', value: 'true' }])
    expect(view.stored.GitHubOAuthEnabled).toBe('true')
  })

  it('retains an unacknowledged secret even if the server may have committed it', async () => {
    let fail = true
    const view = await render({
      beforeWrite(update, stored) {
        if (update.key === 'GitHubClientSecret' && fail) {
          stored[update.key] = update.value
          throw new Error('Connection lost before acknowledgement')
        }
      },
    })
    await view.input('GitHubClientId').setValue('client')
    await view.input('GitHubClientSecret').setValue('secret')
    await view.toggle('GitHubOAuthEnabled')
    await view.save()
    expect(view.input('GitHubClientSecret').element.value).toBe('secret')
    expect(view.stored.GitHubOAuthEnabled).toBe('false')
    fail = false
    view.writes.length = 0
    await view.save()
    expect(view.writes.map(({ key }) => key)).toEqual([
      'GitHubClientSecret',
      'GitHubOAuthEnabled',
    ])
  })
})

describe('built-in OIDC discovery', () => {
  const wellKnown = 'https://oidc.example/.well-known/openid-configuration'
  it('discovers endpoints before any write when first enabling OIDC', async () => {
    const view = await render({ initial: { 'oidc.well_known': wellKnown } })
    let release!: (value: unknown) => void
    view.discovery.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          release = resolve as typeof release
        }) as never
    )
    await view.input('oidc.client_id').setValue('client')
    await view.input('oidc.client_secret').setValue('secret')
    await view.toggle('oidc.enabled')
    await view.save()
    expect(view.writes).toEqual([])
    expect(view.discovery).toHaveBeenCalledExactlyOnceWith(
      '/api/custom-oauth-provider/discovery',
      { well_known_url: wellKnown }
    )
    release({
      well_known_url: wellKnown,
      discovery: {
        authorization_endpoint: 'https://oidc.example/auth',
        token_endpoint: 'https://oidc.example/token',
        userinfo_endpoint: 'https://oidc.example/user',
      },
    })
    await flushPromises()
    expect(view.writes.at(-1)).toEqual({ key: 'oidc.enabled', value: 'true' })
    expect(view.stored['oidc.token_endpoint']).toBe(
      'https://oidc.example/token'
    )
  })

  it.each(['network', 'incomplete', 'invalid endpoint'])(
    'does not write anything on %s discovery failure',
    async (failure) => {
      const view = await render()
      if (failure === 'network')
        view.discovery.mockRejectedValueOnce(new Error('Discovery unavailable'))
      else
        view.discovery.mockResolvedValueOnce({
          well_known_url: wellKnown,
          discovery: {
            authorization_endpoint:
              failure === 'incomplete' ? undefined : 'javascript:bad',
            token_endpoint: 'https://oidc.example/token',
            userinfo_endpoint: 'https://oidc.example/user',
          },
        } as never)
      await view.input('oidc.client_id').setValue('client')
      await view.input('oidc.client_secret').setValue('secret')
      await view.input('oidc.well_known').setValue(wellKnown)
      await view.toggle('oidc.enabled')
      await view.save()
      expect(view.writes).toEqual([])
      expect(view.input('oidc.client_secret').element.value).toBe('secret')
      expect(toast.error).toHaveBeenCalledOnce()
      expect(toast.success).not.toHaveBeenCalled()
    }
  )

  it('does not discover unchanged OIDC when changing another provider', async () => {
    const view = await render({
      initial: { 'oidc.well_known': wellKnown, 'oidc.enabled': 'true' },
    })
    await view.input('GitHubClientId').setValue('other-client')
    await view.save()
    expect(view.discovery).not.toHaveBeenCalled()
    expect(view.writes).toEqual([
      { key: 'GitHubClientId', value: 'other-client' },
    ])
  })

  it('discovers a changed URL while OIDC remains enabled', async () => {
    const view = await render({
      initial: {
        'oidc.enabled': 'true',
        'oidc.well_known': 'https://old.example/discovery',
        'oidc.authorization_endpoint': 'https://oidc.example/auth',
      },
    })
    await view
      .input('oidc.authorization_endpoint')
      .setValue('https://manual.example/auth')
    await view.input('oidc.well_known').setValue(wellKnown)
    await view.save()
    expect(view.discovery).toHaveBeenCalledOnce()
    expect(view.writes.some(({ key }) => key === 'oidc.enabled')).toBe(false)
    expect(view.stored['oidc.authorization_endpoint']).toBe(
      'https://oidc.example/auth'
    )
    expect(
      view.writes.some(({ key }) => key === 'oidc.authorization_endpoint')
    ).toBe(false)
  })

  it('disables first without discovery even when changing the URL', async () => {
    const view = await render({
      initial: {
        'oidc.enabled': 'true',
        'oidc.well_known': wellKnown,
        'oidc.client_id': 'old',
      },
    })
    await view.toggle('oidc.enabled')
    await view.input('oidc.client_id').setValue('')
    await view
      .input('oidc.well_known')
      .setValue('https://unavailable.example/discovery')
    await view.save()
    expect(view.discovery).not.toHaveBeenCalled()
    expect(view.writes[0]).toEqual({ key: 'oidc.enabled', value: 'false' })
    expect(view.stored['oidc.client_id']).toBe('')
  })
})
