vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ refreshStatus: vi.fn().mockResolvedValue(undefined) }),
}))
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import SystemSettingsDomainView from '../SystemSettingsDomainView.vue'
import { useSystemSettings } from '@/composables/useSystemSettings'
import {
  SYSTEM_SETTINGS_DOMAINS,
  type SystemSettingsDomainId,
} from '@/constants/systemSettingsCatalog'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const { backend, toast } = vi.hoisted(() => ({
  backend: {
    options: {} as Record<string, string>,
    puts: [] as Array<{ key: string; value: string }>,
    patches: [] as Array<Record<string, string>>,
    offline: false,
    failKey: '',
    gate: null as Promise<void> | null,
  },
  toast: { error: vi.fn(), success: vi.fn() },
}))

vi.mock('@/composables/useToast', () => ({ useToast: () => toast }))
// GET /api/option/ mirrors controller.GetOptions: credential suffixes are
// withheld and Resend key availability is reported as ResendKeyConfigured.
// Integration panels load their own data and stay on their offline states.
vi.mock('@/api/client', async (importOriginal) => {
  const offline = () => Promise.reject(new Error('offline'))
  const hidden = /(?:Token|Secret|Key|secret|api_key)$/
  // GetRequestPolicy answers GET and PATCH with the full stored snapshot.
  const policyKey = /^(?:RetryTimes|CheckSensitive\w*Enabled|SensitiveWords)$/
  const policy = () => ({
    options: Object.fromEntries(
      Object.entries(backend.options).filter(([key]) => policyKey.test(key))
    ),
  })
  async function patch(
    path: string,
    body: { options: Record<string, string> }
  ) {
    if (path !== '/api/option/request_policy') return offline()
    backend.patches.push(body.options)
    Object.assign(backend.options, body.options)
    return policy()
  }
  async function get(path: string) {
    if (path === '/api/option/request_policy' && !backend.offline)
      return policy()
    if (path !== '/api/option/' || backend.offline) return offline()
    return [
      ...Object.entries(backend.options)
        .filter(([key]) => !hidden.test(key))
        .map(([key, value]) => ({ key, value })),
      {
        key: 'ResendKeyConfigured',
        value: String(Boolean(backend.options.ResendApiKey)),
      },
    ]
  }
  async function put(path: string, body: { key: string; value: string }) {
    if (path !== '/api/option/') return offline()
    backend.puts.push(body)
    if (backend.gate) await backend.gate
    if (body.key === backend.failKey) throw new Error('Option save failed')
    backend.options[body.key] = body.value
    return undefined
  }
  return {
    ...(await importOriginal<typeof import('@/api/client')>()),
    api: { get, post: offline, put, patch, delete: offline },
  }
})

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

// Publishes stored options through the shared composable, as a refresh would.
async function serve(options: Record<string, string>) {
  backend.options = { ...backend.options, ...options }
  expect(await useSystemSettings().load(true)).toBe(true)
}

beforeEach(async () => {
  await setLocale('en')
  Object.assign(backend, {
    options: {
      EpayId: '000123',
      Price: '7.3',
      MinTopUp: '1',
      PayAddress: 'https://pay.example',
      PayMethods: '[]',
    },
    puts: [],
    patches: [],
    offline: false,
    failKey: '',
    gate: null,
  })
  await serve({})
  toast.error.mockClear()
  toast.success.mockClear()
})

async function render(
  domain: SystemSettingsDomainId = 'billing',
  section = 'epay'
) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: `/${domain}/:section?`,
        name: `system-settings-${domain}`,
        component: SystemSettingsDomainView,
        props: { domain },
      },
      { path: '/ops', name: 'ops-monitor', component: { render: () => null } },
    ],
  })
  await router.push(`/${domain}/${section}`)
  const wrapper = mount(SystemSettingsDomainView, {
    props: { domain },
    global: { plugins: [i18n, router] },
  })
  await flushPromises()
  return wrapper
}

describe('EPay settings page', () => {
  it.each([
    {
      locale: 'en',
      settlementLabel: 'Enable Epay settlement',
      settlementPromise: 'settlement switch',
      availability:
        'New orders and payment notifications require payment compliance confirmation, complete merchant credentials, and at least one payment method.',
      emptyMethods:
        'An empty list disables EPay orders and payment notifications.',
      saveLabel: 'Save changes',
    },
    {
      locale: 'zh-CN',
      settlementLabel: '启用易支付结算',
      settlementPromise: '结算开关',
      availability:
        '创建新单和处理支付回调均需完成支付合规确认、填写完整商户凭据，并配置至少一种支付方式。',
      emptyMethods: '清空列表后停用 EPay 新单及支付回调。',
      saveLabel: '保存更改',
    },
  ])(
    'does not promise or resubmit independent EPay settlement in $locale',
    async ({
      locale,
      settlementLabel,
      settlementPromise,
      availability,
      emptyMethods,
      saveLabel,
    }) => {
      await setLocale(locale)
      await serve({ 'payment_setting.epay_settlement_enabled': 'false' })
      const wrapper = await render()
      expect(
        wrapper
          .find(`[role="switch"][aria-label="${settlementLabel}"]`)
          .exists()
      ).toBe(false)
      expect(wrapper.text()).not.toContain(settlementLabel)
      expect(wrapper.text()).not.toContain(settlementPromise)
      expect(wrapper.text()).toContain(availability)
      expect(wrapper.text()).toContain(emptyMethods)
      await wrapper.get('[id="setting-Price"]').setValue('8')
      await wrapper
        .findAll('button')
        .find((button) => button.text() === saveLabel)!
        .trigger('click')
      await flushPromises()
      expect(backend.puts).toEqual([{ key: 'Price', value: '8' }])
      wrapper.unmount()
    }
  )

  it('disables failed-load drafts and enables fields only after retrying authoritative settings', async () => {
    backend.offline = true
    expect(await useSystemSettings().load(true)).toBe(false)
    const wrapper = await render()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'could not be loaded'
    )
    backend.offline = false
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Reload settings')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayId"]').element.value
    ).toBe('000123')
    expect(backend.puts).toEqual([])
    wrapper.unmount()
  })

  it('preserves credentials without a confirmed write and clears them after successful saving', async () => {
    const wrapper = await render()
    await wrapper.get('[id="setting-EpayKey"]').setValue('new-secret')
    await wrapper.get('[id="setting-Price"]').setValue('8')
    backend.failKey = 'EpayKey'
    const save = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save changes')!
    await save.trigger('click')
    await flushPromises()
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayKey"]').element.value
    ).toBe('new-secret')
    expect(backend.puts).toEqual([{ key: 'EpayKey', value: 'new-secret' }])
    backend.failKey = ''
    await save.trigger('click')
    await flushPromises()
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayKey"]').element.value
    ).toBe('')
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-Price"]').element.value
    ).toBe('8')
    expect(backend.options).toMatchObject({ EpayKey: 'new-secret', Price: '8' })
    expect(save.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('sends only one request while saving and keeps inputs locked until completion', async () => {
    const wrapper = await render()
    let finish!: () => void
    backend.gate = new Promise((resolve) => {
      finish = resolve
    })
    backend.failKey = 'EpayId'
    await wrapper.get('[id="setting-EpayId"]').setValue('merchant-2')
    const save = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save changes')!
    await save.trigger('click')
    await save.trigger('click')
    expect(backend.puts).toEqual([{ key: 'EpayId', value: 'merchant-2' }])
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    finish()
    await flushPromises()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayId"]').element.value
    ).toBe('merchant-2')
    wrapper.unmount()
  })

  it('refreshes an untouched form but preserves an edited draft during shared refreshes', async () => {
    const wrapper = await render()
    await serve({ EpayId: 'server-change' })
    await flushPromises()
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayId"]').element.value
    ).toBe('server-change')
    await wrapper.get('[id="setting-Price"]').setValue('9')
    await serve({ EpayId: 'newer-server-change' })
    await flushPromises()
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-Price"]').element.value
    ).toBe('9')
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayId"]').element.value
    ).toBe('server-change')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save changes')!
      .trigger('click')
    await flushPromises()
    expect(backend.puts).toEqual([{ key: 'Price', value: '9' }])
    wrapper.unmount()
  })

  it('shows invalid stored values as field errors and refuses to save them', async () => {
    await serve({ Price: 'bad' })
    const wrapper = await render()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
    expect(wrapper.get('.settings-error').text()).toBe('Enter a finite number')
    await wrapper.get('[id="setting-EpayId"]').setValue('merchant-2')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save changes')!
      .trigger('click')
    await flushPromises()
    expect(toast.error).toHaveBeenCalledExactlyOnceWith(
      'Correct invalid settings before saving.'
    )
    expect(backend.puts).toEqual([])
    wrapper.unmount()
  })

  it('filters fields in the active settings section without changing the save scope', async () => {
    const wrapper = await render()
    const search = wrapper.get('input[type="search"]')
    await search.setValue('merchant')
    expect(wrapper.find('[id="setting-EpayId"]').exists()).toBe(true)
    expect(wrapper.find('[id="setting-Price"]').exists()).toBe(false)
    await search.setValue('does-not-exist')
    expect(wrapper.get('.settings-search-empty').text()).toContain(
      'No settings match this search'
    )
    wrapper.unmount()
  })
})

describe('GMPay settings page', () => {
  it.each([
    ['en', 'Enable crypto settlement', 'Enable GMPay', 'Save changes'],
    ['zh-CN', '启用虚拟币结算', '启用 GMPay', '保存更改'],
  ])(
    'does not expose or resubmit the retired settlement option in %s',
    async (locale, retiredLabel, newOrdersLabel, saveLabel) => {
      await setLocale(locale)
      await serve({ 'payment_setting.gmpay_settlement_enabled': 'false' })
      const wrapper = await render('billing', 'gmpay')
      expect(wrapper.text()).not.toContain(retiredLabel)
      expect(
        wrapper
          .find('[id="setting-payment_setting.gmpay_settlement_enabled"]')
          .exists()
      ).toBe(false)
      expect(wrapper.text()).toContain(newOrdersLabel)
      const save = wrapper
        .findAll('button')
        .find((button) => button.text() === saveLabel)!
      expect(save.attributes('disabled')).toBeDefined()
      await save.trigger('click')
      await flushPromises()
      expect(backend.puts).toEqual([])
      wrapper.unmount()
    }
  )
})

describe('Email settings page', () => {
  // The backend validates the stored Resend key and sender when the method
  // switches, so a single save must write both before EmailProvider.
  it('writes Resend credentials before switching the sending method', async () => {
    const wrapper = await render('operations', 'email')
    await wrapper.get('[id="setting-EmailProvider"]').setValue('resend')
    await wrapper.get('[id="setting-ResendApiKey"]').setValue('re_test')
    await wrapper
      .get('[id="setting-ResendFrom"]')
      .setValue('"Site" <noreply@example.com>')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save changes')!
      .trigger('click')
    await flushPromises()
    expect(backend.puts).toEqual([
      { key: 'ResendFrom', value: '"Site" <noreply@example.com>' },
      { key: 'ResendApiKey', value: 're_test' },
      { key: 'EmailProvider', value: 'resend' },
    ])
    wrapper.unmount()
  })

  it('keeps the hidden Turnstile site key when saving it empty', async () => {
    await serve({
      TurnstileSiteKey: 'site-key',
      TurnstileCheckEnabled: 'false',
    })
    const wrapper = await render('auth', 'bot-protection')
    const siteKey = wrapper.get('[id="setting-TurnstileSiteKey"]')
    expect((siteKey.element as HTMLInputElement).value).toBe('')
    expect(wrapper.text()).toContain(
      'The saved Site Key is not shown here; saving it empty keeps the current value.'
    )
    await wrapper
      .get('[role="switch"][aria-label="Enable Turnstile"]')
      .trigger('click')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save changes')!
      .trigger('click')
    await flushPromises()
    expect(backend.puts).toEqual([
      { key: 'TurnstileCheckEnabled', value: 'true' },
    ])
    expect(backend.options.TurnstileSiteKey).toBe('site-key')
    wrapper.unmount()
  })
})

describe('request policy panel', () => {
  it('shows a policy saved in the panel on the settings form', async () => {
    await serve({
      CheckSensitiveEnabled: 'false',
      SensitiveWords: 'old',
      RetryTimes: '2',
    })
    const wrapper = await render('security', 'sensitive')
    const sensitiveCheck = () =>
      wrapper.get(
        '[role="switch"][aria-label="Enable sensitive word detection"]'
      )
    expect(sensitiveCheck().attributes('aria-checked')).toBe('false')
    const policy = wrapper.get('textarea[aria-label="Request policy"]')
    const draft = JSON.parse((policy.element as HTMLTextAreaElement).value)
    await policy.setValue(
      JSON.stringify({ ...draft, CheckSensitiveEnabled: 'true' })
    )
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save request policy')!
      .trigger('click')
    await flushPromises()
    expect(backend.patches).toEqual([{ CheckSensitiveEnabled: 'true' }])
    expect(sensitiveCheck().attributes('aria-checked')).toBe('true')
    wrapper.unmount()
  })
})

describe('system settings in English', () => {
  it('renders every section without Chinese text or raw message keys', async () => {
    for (const domain of SYSTEM_SETTINGS_DOMAINS) {
      for (const section of domain.sections) {
        const wrapper = await render(domain.id, section.id)
        const html = wrapper.html()
        const where = `${domain.id}/${section.id}`
        // Han script plus CJK and full-width punctuation.
        expect(html, where).not.toMatch(
          /[\p{Script=Han}\u3000-\u303f\uff00-\uffef]/u
        )
        expect(html, where).not.toContain('systemSettings.')
        wrapper.unmount()
      }
    }
  })
})
