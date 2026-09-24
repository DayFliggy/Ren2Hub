import { flushPromises, mount } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'
import { api } from '@/api/console'
import GmpayPanel from '../GmpayPanel.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const settings = vi.hoisted(() => ({
  configured: true,
  values: {} as Record<string, string | number>,
  load: vi.fn(),
}))
const toast = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }))
vi.mock('@/composables/useSystemSettings', () => ({
  useSystemSettings: () => ({
    load: settings.load,
    rawValue: (key: string, fallback: unknown) =>
      settings.values[key] ?? fallback,
    isSecretConfigured: () => settings.configured,
  }),
}))
vi.mock('@/composables/useToast', () => ({ useToast: () => toast }))

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  settings.configured = true
  settings.values = {
    GmpayPayAddress: 'https://pay.example',
    GmpayId: '1000',
    GmpayPrice: 1,
    GmpayMinTopUp: 1,
    GmpayGroupRatio: '{"default":1,"vip":0.8}',
    GmpayAssets: JSON.stringify([
      {
        name: 'USDT Tron',
        token: 'usdt',
        network: 'tron',
        icon: 'usdt',
        color: '#123456',
      },
      { name: 'USDC Ethereum', token: 'usdc', network: 'ethereum' },
    ]),
  }
  settings.load.mockResolvedValue(true)
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.clearAllMocks()
})

async function render() {
  const wrapper = mount(GmpayPanel, { global: { plugins: [i18n] } })
  await flushPromises()
  return wrapper
}

describe('GMPay administration', () => {
  it('edits optional asset metadata without changing its token and network', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({})
    const wrapper = await render()
    const fields = wrapper.findAllComponents({ name: 'FormField' })
    const icon = fields.find((field) =>
      String(field.props('label')).includes('USDT · tron icon identifier')
    )!
    const color = fields.find((field) =>
      String(field.props('label')).includes('USDT · tron color metadata')
    )!
    await icon.get('input').setValue('coin-usdt')
    await color.get('input').setValue('#246810')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save gateway configuration')!
      .trigger('click')
    await flushPromises()
    const payload = post.mock.calls[0]?.[1] as { assets: string }
    expect(JSON.parse(payload.assets)[0]).toEqual({
      name: 'USDT Tron',
      token: 'usdt',
      network: 'tron',
      icon: 'coin-usdt',
      color: '#246810',
    })
    expect(wrapper.text()).toContain('/api/user/gmpay/notify')
    wrapper.unmount()
  })
  it.each(['invalid-json', '{}', '[{"token":"usdt"}]'])(
    'never replaces a malformed stored catalog with an empty selection: %s',
    async (value) => {
      settings.values.GmpayAssets = value
      const post = vi.spyOn(api, 'post')
      const wrapper = await render()
      expect(wrapper.get('[role="alert"]').text()).toContain(
        'could not be loaded'
      )
      expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
      expect(post).not.toHaveBeenCalled()
      wrapper.unmount()
    }
  )
  it('blocks editing until configuration loads and supports an explicit retry after failure', async () => {
    let finish!: (ok: boolean) => void
    settings.load.mockImplementationOnce(
      () =>
        new Promise<boolean>((resolve) => {
          finish = resolve
        })
    )
    const post = vi.spyOn(api, 'post')
    const wrapper = mount(GmpayPanel, { global: { plugins: [i18n] } })
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[role="status"]').text()).toContain('Loading gateway')
    finish(false)
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'could not be loaded'
    )
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(post).not.toHaveBeenCalled()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Reload gateway configuration')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
    expect(
      wrapper.get<HTMLInputElement>('input[type="url"]').element.value
    ).toBe('https://pay.example')
    wrapper.unmount()
  })

  it('keeps the saved draft visible when refreshing fails, without permitting a stale second save', async () => {
    settings.load.mockResolvedValueOnce(true).mockResolvedValueOnce(false)
    const post = vi.spyOn(api, 'post').mockResolvedValue({})
    const wrapper = await render()
    await wrapper.get('input[type="url"]').setValue('https://new-pay.example')
    await wrapper.get('input[type="password"]').setValue('replacement-secret')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save gateway configuration')!
      .trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledOnce()
    expect(
      wrapper.get<HTMLInputElement>('input[type="url"]').element.value
    ).toBe('https://new-pay.example')
    expect(
      wrapper.get<HTMLInputElement>('input[type="password"]').element.value
    ).toBe('')
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'could not be loaded'
    )
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })

  it('sends whole-number asset minimums in the decimal string format required by the API', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({})
    const wrapper = await render()
    await wrapper.findAll('input[type="number"]').at(2)!.setValue('1e2')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save gateway configuration')!
      .trigger('click')
    await flushPromises()
    const payload = post.mock.calls[0]?.[1] as { assets: string }
    expect(JSON.parse(payload.assets)[0].min_topup).toBe('100')
    wrapper.unmount()
  })

  it('sends one configuration write while saving and ignores completion after leaving the panel', async () => {
    let finish!: (value: unknown) => void
    const post = vi.spyOn(api, 'post').mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        })
    )
    const wrapper = await render()
    const save = wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save gateway configuration')!
    await save.trigger('click')
    await save.trigger('click')
    expect(post).toHaveBeenCalledOnce()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    wrapper.unmount()
    finish({})
    await flushPromises()
    expect(settings.load).toHaveBeenCalledWith(true)
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(toast.success).not.toHaveBeenCalled()
  })

  it('removes saved assets without probing and preserves payment metadata and the existing secret', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({})
    const wrapper = await render()
    expect(wrapper.text()).toContain('Gateway address')
    await wrapper.get('[aria-label="Remove USDC · ethereum"]').trigger('click')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save gateway configuration')!
      .trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledOnce()
    expect(post).toHaveBeenCalledWith(
      '/api/option/gmpay/save',
      expect.objectContaining({
        secret_key: '',
        assets: JSON.stringify([
          {
            name: 'USDT Tron',
            token: 'usdt',
            network: 'tron',
            icon: 'usdt',
            color: '#123456',
          },
        ]),
        group_ratio: '{"default":1,"vip":0.8}',
      })
    )
    expect(settings.load).toHaveBeenCalledWith(true)
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })

  it('probes only the public address and ignores a response after the address changes', async () => {
    let finish!: (value: unknown) => void
    const post = vi.spyOn(api, 'post').mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve
        })
    )
    const wrapper = await render()
    await wrapper.get('input[type="password"]').setValue('test-secret')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Load available assets')!
      .trigger('click')
    expect(post).toHaveBeenCalledWith(
      '/api/option/gmpay/probe',
      { pay_address: 'https://pay.example' },
      expect.anything()
    )
    await wrapper.get('input[type="url"]').setValue('https://new-pay.example')
    expect(post.mock.calls[0]?.[2]?.signal?.aborted).toBe(true)
    finish({
      supported_assets: [
        { network: 'tron', display_name: 'Old gateway', tokens: ['usdt'] },
      ],
    })
    await flushPromises()
    expect(wrapper.text()).not.toContain('Old gateway')
    expect(toast.success).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('can stop new crypto orders by saving an empty asset catalog', async () => {
    const post = vi.spyOn(api, 'post').mockResolvedValue({})
    const wrapper = await render()
    await wrapper.get('[aria-label="Remove USDC · ethereum"]').trigger('click')
    await wrapper.get('[aria-label="Remove USDT · tron"]').trigger('click')
    expect(wrapper.text()).toContain('stops new crypto orders')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save gateway configuration')!
      .trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(
      '/api/option/gmpay/save',
      expect.objectContaining({ assets: '[]' })
    )
    wrapper.unmount()
  })

  it.each(['not-json', 'null', '[]', '{"vip":0}', '{"vip":"0.5"}'])(
    'rejects invalid group pricing %s',
    async (value) => {
      settings.values.GmpayGroupRatio = value
      const post = vi.spyOn(api, 'post')
      const wrapper = await render()
      await wrapper
        .findAll('button')
        .find((button) => button.text() === 'Save gateway configuration')!
        .trigger('click')
      expect(post).not.toHaveBeenCalled()
      expect(toast.error).toHaveBeenCalledWith(
        'Enter a JSON object with positive numeric group multipliers.'
      )
      wrapper.unmount()
    }
  )

  it('normalizes catalog identities and lets the administrator deselect a saved asset', async () => {
    vi.spyOn(api, 'post').mockResolvedValue({
      supported_assets: [
        { network: 'TRON', display_name: 'Tron', tokens: ['USDT'] },
      ],
    })
    const wrapper = await render()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Load available assets')!
      .trigger('click')
    await flushPromises()
    const asset = wrapper.get('button[aria-pressed]')
    expect(asset.attributes('aria-pressed')).toBe('true')
    await asset.trigger('click')
    expect(asset.attributes('aria-pressed')).toBe('false')
    expect(wrapper.find('[aria-label="Remove USDT · tron"]').exists()).toBe(
      false
    )
    wrapper.unmount()
  })

  it('rejects invalid prices without publishing a configuration', async () => {
    const post = vi.spyOn(api, 'post')
    const wrapper = await render()
    await wrapper.get('input[type="number"]').setValue('0')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save gateway configuration')!
      .trigger('click')
    expect(post).not.toHaveBeenCalled()
    expect(toast.error).toHaveBeenCalledWith(
      expect.stringContaining('price must be positive')
    )
    wrapper.unmount()
  })

  it('reports malformed catalogs without offering payment assets', async () => {
    vi.spyOn(api, 'post').mockResolvedValue({
      supported_assets: [
        { network: 'tron', display_name: 'Tron', tokens: [7] },
      ],
    })
    const wrapper = await render()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Load available assets')!
      .trigger('click')
    await flushPromises()
    expect(toast.error).toHaveBeenCalledWith(
      'The gateway returned an invalid asset catalog.'
    )
    expect(wrapper.find('button[aria-pressed]').exists()).toBe(false)
    wrapper.unmount()
  })
})
