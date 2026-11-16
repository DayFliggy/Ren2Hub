import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { useSystemSettings } from '@/composables/useSystemSettings'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import SystemSettingsDomainView from '../SystemSettingsDomainView.vue'
import { parseTopupBonusSettings } from '@/api/topupBonus'
import configResponse from '../../../../../tests/fixtures/native/topup-bonus.get.json'

const mocks = vi.hoisted(() => ({
  options: {} as Record<string, string>,
  puts: [] as Array<{ key: string; value: string }>,
  failKey: '',
  refreshStatus: vi.fn(),
  bonusLoad: vi.fn(),
  bonusSave: vi.fn(),
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}))
vi.mock('@/api/topupBonus', async (original) => ({
  ...(await original<typeof import('@/api/topupBonus')>()),
  topupBonusApi: { load: mocks.bonusLoad, save: mocks.bonusSave },
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ refreshStatus: mocks.refreshStatus }),
}))
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks.toast }))
vi.mock('@/api/client', async (importOriginal) => ({
  ...(await importOriginal<typeof import('@/api/client')>()),
  api: {
    get: vi.fn(async (path: string) => {
      if (path === '/api/option/')
        return Object.entries(mocks.options).map(([key, value]) => ({
          key,
          value,
        }))
      if (path === '/api/option/gmpay/config')
        return {
          pay_address: 'https://pay.test',
          merchant_id: 'merchant',
          has_secret: true,
          enabled: true,
          assets: [],
        }
      if (path === '/api/option/request_policy')
        return { options: { RetryTimes: '2' } }
      if (path === '/api/custom-oauth-provider/') return []
      throw new Error('unsupported test endpoint')
    }),
    put: vi.fn(async (_path: string, body: { key: string; value: string }) => {
      mocks.puts.push(body)
      if (body.key === mocks.failKey) throw new Error('write rejected')
      mocks.options[body.key] = body.value
    }),
  },
}))
const wrappers: VueWrapper[] = []
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(async () => {
  vi.clearAllMocks()
  mocks.refreshStatus.mockResolvedValue(undefined)
  mocks.bonusLoad.mockResolvedValue(
    parseTopupBonusSettings(configResponse.data)
  )
  mocks.options = {
    SystemName: 'Original site',
    Logo: 'https://example.test/old.svg',
    EpayId: 'old-merchant',
    Price: '7.3',
    PayMethods: '[]',
    'console_setting.announcements': '[]',
  }
  mocks.puts = []
  mocks.failKey = ''
  await useSystemSettings().load(true)
  await useSystemSettings().refreshStatus()
})
afterEach(() => {
  wrappers.splice(0).forEach((wrapper) => wrapper.unmount())
  vi.restoreAllMocks()
})
async function render(domain = 'billing', section = 'epay') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/system-settings/:domain/:section',
        name: 'settings-test',
        component: SystemSettingsDomainView,
        props: (route) => ({ domain: route.params.domain }),
      },
      { path: '/other', component: { template: '<p>Other page</p>' } },
    ],
  })
  // Domain's section links use the real named-route contract.
  for (const item of ['billing', 'site', 'auth', 'content', 'security'])
    router.addRoute({
      path: `/system-settings/${item}/:section`,
      name: `system-settings-${item}`,
      component: SystemSettingsDomainView,
      props: { domain: item },
    })
  await router.push('/other')
  await router.push(`/system-settings/${domain}/${section}`)
  const wrapper = mount(RouterView, { global: { plugins: [router, i18n] } })
  wrappers.push(wrapper)
  await flushPromises()
  return { wrapper, router }
}
function save(wrapper: VueWrapper) {
  return wrapper
    .findAll('button')
    .find((button) => button.text() === 'Save changes')!
}

describe('settings draft leave protection', () => {
  it('cancels section, domain, page and history-back navigation without losing the draft', async () => {
    const { wrapper, router } = await render()
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    await wrapper.get('[id="setting-EpayId"]').setValue('unsaved-merchant')
    await wrapper
      .findAll('button')
      .find(
        (button) =>
          button.text() === i18n.global.t('systemSettings.gmpayPanel.title')
      )!
      .trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/system-settings/billing/epay')
    await router.push('/system-settings/site/system-info')
    await router.push('/other')
    router.back()
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/system-settings/billing/epay')
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayId"]').element.value
    ).toBe('unsaved-merchant')
    expect(confirm).toHaveBeenCalledTimes(4)
    const unload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(unload)
    expect(unload.defaultPrevented).toBe(true)
    confirm.mockReturnValue(true)
    await router.push('/system-settings/billing/gmpay')
    await flushPromises()
    await router.push('/system-settings/billing/epay')
    await flushPromises()
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayId"]').element.value
    ).toBe('old-merchant')
  })

  it('does not prompt after a successful save or when no draft exists', async () => {
    const { wrapper, router } = await render()
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    await wrapper.get('[id="setting-EpayId"]').setValue('saved-merchant')
    await save(wrapper).trigger('click')
    await flushPromises()
    await router.push('/other')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/other')
    expect(confirm).not.toHaveBeenCalled()
    const unload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(unload)
    expect(unload.defaultPrevented).toBe(false)
  })

  it('retains only uncommitted draft fields after partial save and a failed status refresh', async () => {
    const { wrapper, router } = await render('site', 'system-info')
    await wrapper.get('[id="setting-SystemName"]').setValue('Committed site')
    await wrapper
      .get('[id="setting-Logo"]')
      .setValue('https://example.test/new.svg')
    mocks.failKey = 'Logo'
    mocks.refreshStatus.mockRejectedValueOnce(new Error('status offline'))
    await save(wrapper).trigger('click')
    await flushPromises()
    expect(mocks.options.SystemName).toBe('Committed site')
    expect(mocks.options.Logo).toBe('https://example.test/old.svg')
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-Logo"]').element.value
    ).toBe('https://example.test/new.svg')
    expect(wrapper.text()).toContain('Settings were saved')
    expect(wrapper.text()).toContain('status offline')
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    await router.push('/other')
    expect(confirm).toHaveBeenCalledOnce()
    const count = mocks.puts.length
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Retry site refresh')!
      .trigger('click')
    await flushPromises()
    expect(mocks.puts).toHaveLength(count)
    expect(wrapper.text()).not.toContain('status offline')
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-Logo"]').element.value
    ).toBe('https://example.test/new.svg')
    mocks.failKey = ''
    await save(wrapper).trigger('click')
    await flushPromises()
    expect(mocks.puts.slice(count)).toEqual([
      { key: 'Logo', value: 'https://example.test/new.svg' },
    ])
  })

  it('protects a dedicated GMPay draft even when the surrounding section has no fields', async () => {
    const { wrapper, router } = await render('billing', 'gmpay')
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    await wrapper.get('input[type=password]').setValue('unsaved-test-secret')
    await router.push('/system-settings/billing/epay')
    expect(router.currentRoute.value.path).toBe(
      '/system-settings/billing/gmpay'
    )
    expect(confirm).toHaveBeenCalledOnce()
    expect(
      wrapper.get<HTMLInputElement>('input[type=password]').element.value
    ).toBe('unsaved-test-secret')
  })

  it('protects system bonus drafts once across section changes, and releases the guard after saving', async () => {
    const { wrapper, router } = await render('billing', 'topup-bonus')
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const fixed = wrapper.get<HTMLInputElement>('input[id$="-fixed"]')
    await fixed.setValue('1')
    await wrapper
      .findAll('.settings-section-link')
      .find((button) => button.text() === 'EPay')!
      .trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe(
      '/system-settings/billing/topup-bonus'
    )
    expect(confirm).toHaveBeenCalledOnce()
    expect(fixed.element.value).toBe('1')
    const unload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(unload)
    expect(unload.defaultPrevented).toBe(true)
    let finish!: (value: unknown) => void
    mocks.bonusSave.mockImplementation(
      (_version, config) =>
        new Promise((resolve) => {
          finish = () =>
            resolve({
              ...parseTopupBonusSettings(configResponse.data),
              version: 1,
              config,
            })
        })
    )
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save configuration')!
      .trigger('click')
    await router.push('/system-settings/billing/epay')
    expect(router.currentRoute.value.path).toBe(
      '/system-settings/billing/topup-bonus'
    )
    expect(confirm).toHaveBeenCalledOnce()
    finish(undefined)
    await flushPromises()
    await router.push('/system-settings/billing/epay')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/system-settings/billing/epay')
    expect(confirm).toHaveBeenCalledOnce()
    const cleanUnload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(cleanUnload)
    expect(cleanUnload.defaultPrevented).toBe(false)
  })
})

describe('settings draft field kinds', () => {
  it('protects a toggle change and a JSON draft without saving either', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const toggle = await render('auth', 'basic-auth')
    await toggle.wrapper.findAll('[role=switch]')[0]!.trigger('click')
    await toggle.router.push('/other')
    expect(toggle.router.currentRoute.value.path).toBe(
      '/system-settings/auth/basic-auth'
    )
    toggle.wrapper.unmount()
    const json = await render('content', 'console-content')
    await json.wrapper
      .findAll('.collection-editor button')
      .find((button) => button.text() === 'JSON')!
      .trigger('click')
    await json.wrapper
      .get('textarea')
      .setValue('[{"title":"Unsaved announcement"}]')
    await json.router.push('/other')
    expect(json.router.currentRoute.value.path).toBe(
      '/system-settings/content/console-content'
    )
    expect(
      json.wrapper.get<HTMLTextAreaElement>('textarea').element.value
    ).toContain('Unsaved announcement')
    expect(confirm).toHaveBeenCalledTimes(2)
    expect(mocks.puts).toEqual([])
  })

  it('protects closing a custom OAuth provider draft as well as route navigation', async () => {
    const { wrapper, router } = await render('auth', 'custom-oauth')
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    await wrapper
      .findAll('button')
      .find(
        (button) =>
          button.text() === i18n.global.t('systemSettings.customOAuth.add')
      )!
      .trigger('click')
    await flushPromises()
    const panel = wrapper.findComponent({ name: 'CustomOAuthProvidersPanel' })
    const dialog = panel.findComponent({ name: 'ConsoleModal' })
    const body = document.querySelector('[role=dialog]')
    expect(body).not.toBeNull()
    const input = body!.querySelector('input')!
    input.value = 'Unsaved provider'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    dialog.vm.$emit('close')
    await flushPromises()
    expect(dialog.props('open')).toBe(true)
    await router.push('/other')
    expect(router.currentRoute.value.path).toBe(
      '/system-settings/auth/custom-oauth'
    )
    expect(confirm).toHaveBeenCalledTimes(2)
    confirm.mockReturnValue(true)
    dialog.vm.$emit('close')
    await flushPromises()
    expect(dialog.props('open')).toBe(false)
  })
})
