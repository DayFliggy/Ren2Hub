import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { readonly, ref } from 'vue'
import SystemSettingsDomainView from '../SystemSettingsDomainView.vue'
import { useSystemSettings } from '@/composables/useSystemSettings'
import {
  SYSTEM_SETTINGS_DOMAINS,
  type SystemSettingsDomainId,
} from '@/constants/systemSettingsCatalog'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

vi.mock('@/composables/useSystemSettings', () => ({
  useSystemSettings: vi.fn(),
}))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn(), success: vi.fn() }),
}))
// Integration panels load their own data; an offline API keeps them on their
// empty or failed states, which still render their static copy.
vi.mock('@/api/client', async (importOriginal) => {
  const offline = () => Promise.reject(new Error('offline'))
  return {
    ...(await importOriginal<typeof import('@/api/client')>()),
    api: { get: offline, post: offline, put: offline, delete: offline },
  }
})

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})

let state: ReturnType<typeof configure>
function configure() {
  const loaded = ref(false)
  const loading = ref(false)
  const saving = ref(false)
  const rawOptions = ref<Record<string, string>>({
    EpayId: '000123',
    Price: '7.3',
    MinTopUp: '1',
    PayAddress: 'https://pay.example',
    PayMethods: '[]',
  })
  const load = vi.fn(async () => {
    loaded.value = true
    return true
  })
  const saveOptions = vi.fn(
    async (patch: Record<string, string | number | boolean>) => {
      rawOptions.value = {
        ...rawOptions.value,
        ...Object.fromEntries(
          Object.entries(patch)
            .filter(([key]) => key !== 'EpayKey')
            .map(([key, value]) => [key, String(value)])
        ),
      }
      return true
    }
  )
  const value = {
    loaded: readonly(loaded),
    loading: readonly(loading),
    saving: readonly(saving),
    rawOptions: readonly(rawOptions),
    load,
    saveOptions,
    rawValue: (key: string, fallback: unknown) => {
      const raw = rawOptions.value[key]
      if (raw === undefined) return fallback
      return typeof fallback === 'number'
        ? Number(raw)
        : typeof fallback === 'boolean'
          ? raw === 'true'
          : raw
    },
    isSecretConfigured: () => true,
  }
  vi.mocked(useSystemSettings).mockReturnValue(
    value as unknown as ReturnType<typeof useSystemSettings>
  )
  return { loaded, loading, saving, rawOptions, load, saveOptions }
}
beforeEach(async () => {
  await setLocale('en')
  state = configure()
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
      state.rawOptions.value['payment_setting.epay_settlement_enabled'] =
        'false'
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
      expect(state.saveOptions).toHaveBeenCalledExactlyOnceWith({ Price: 8 })
      wrapper.unmount()
    }
  )

  it('disables failed-load drafts and enables fields only after retrying authoritative settings', async () => {
    state.load.mockResolvedValueOnce(false)
    const wrapper = await render()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'could not be loaded'
    )
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Reload settings')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayId"]').element.value
    ).toBe('000123')
    expect(state.saveOptions).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('preserves unsaved credentials on failure and clears them only after a successful atomic save', async () => {
    const wrapper = await render()
    await wrapper.get('[id="setting-EpayKey"]').setValue('new-secret')
    await wrapper.get('[id="setting-Price"]').setValue('8')
    state.saveOptions.mockResolvedValueOnce(false)
    const save = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save changes')!
    await save.trigger('click')
    await flushPromises()
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayKey"]').element.value
    ).toBe('new-secret')
    expect(state.saveOptions).toHaveBeenCalledWith({
      EpayKey: 'new-secret',
      Price: 8,
    })
    await save.trigger('click')
    await flushPromises()
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayKey"]').element.value
    ).toBe('')
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-Price"]').element.value
    ).toBe('8')
    expect(save.attributes('disabled')).toBeDefined()
    wrapper.unmount()
  })

  it('sends only one request while saving and keeps inputs locked until completion', async () => {
    const wrapper = await render()
    let finish!: (value: boolean) => void
    state.saveOptions.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve
        })
    )
    await wrapper.get('[id="setting-EpayId"]').setValue('merchant-2')
    const save = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save changes')!
    await save.trigger('click')
    await save.trigger('click')
    expect(state.saveOptions).toHaveBeenCalledOnce()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    finish(false)
    await flushPromises()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayId"]').element.value
    ).toBe('merchant-2')
    wrapper.unmount()
  })

  it('refreshes an untouched form but preserves an edited draft during shared refreshes', async () => {
    const wrapper = await render()
    state.rawOptions.value = {
      ...state.rawOptions.value,
      EpayId: 'server-change',
    }
    await flushPromises()
    expect(
      wrapper.get<HTMLInputElement>('[id="setting-EpayId"]').element.value
    ).toBe('server-change')
    await wrapper.get('[id="setting-Price"]').setValue('9')
    state.rawOptions.value = {
      ...state.rawOptions.value,
      EpayId: 'newer-server-change',
    }
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
    expect(state.saveOptions).toHaveBeenCalledWith({ Price: 9 })
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
