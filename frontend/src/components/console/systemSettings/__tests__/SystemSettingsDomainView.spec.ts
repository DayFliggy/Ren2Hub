import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { readonly, ref } from 'vue'
import SystemSettingsDomainView from '../SystemSettingsDomainView.vue'
import { useSystemSettings } from '@/composables/useSystemSettings'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

vi.mock('@/composables/useSystemSettings', () => ({
  useSystemSettings: vi.fn(),
}))
vi.mock('@/composables/useToast', () => ({
  useToast: () => ({ error: vi.fn(), success: vi.fn() }),
}))

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
beforeEach(() => {
  state = configure()
})

async function render() {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/billing/:section?',
        name: 'system-settings-billing',
        component: SystemSettingsDomainView,
        props: { domain: 'billing' },
      },
    ],
  })
  await router.push('/billing/epay')
  const wrapper = mount(SystemSettingsDomainView, {
    props: { domain: 'billing' },
    global: { plugins: [i18n, router] },
  })
  await flushPromises()
  return wrapper
}

describe('EPay settings page', () => {
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
})
