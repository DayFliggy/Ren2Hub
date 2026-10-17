import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/api/client', () => ({ api }))
import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import GmpayPanel from '../GmpayPanel.vue'
const config = {
  pay_address: 'https://pay.example',
  merchant_id: 'merchant',
  has_secret: true,
  enabled: true,
  assets: [
    {
      name: 'USDT',
      token: 'usdt',
      network: 'tron',
      icon: 'coin',
      color: '#008800',
    },
  ],
}
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  vi.clearAllMocks()
  api.get.mockResolvedValue(structuredClone(config))
  api.post.mockResolvedValue({})
})
async function render() {
  const wrapper = mount(GmpayPanel, { global: { plugins: [i18n] } })
  await flushPromises()
  return wrapper
}
function button(wrapper: Awaited<ReturnType<typeof render>>, text: string) {
  return wrapper.findAll('button').find((b) => b.text() === text)!
}
describe('native GMPay configuration', () => {
  it('loads the merchant snapshot and preserves secret and asset metadata on save', async () => {
    const wrapper = await render()
    expect(api.get).toHaveBeenCalledWith(
      '/api/option/gmpay/config',
      undefined,
      { signal: expect.any(AbortSignal) }
    )
    expect(wrapper.get('input[type=password]').element).toHaveProperty(
      'value',
      ''
    )
    await button(wrapper, 'Save gateway configuration').trigger('click')
    await flushPromises()
    expect(api.post).toHaveBeenCalledWith(
      '/api/option/gmpay/save',
      {
        pay_address: config.pay_address,
        merchant_id: 'merchant',
        secret_key: '',
        enabled: true,
        assets: config.assets,
      },
      { signal: expect.any(AbortSignal) }
    )
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })
  it('blocks writes after a configuration read fails and supports retry', async () => {
    api.get.mockRejectedValueOnce(new Error('unavailable'))
    const wrapper = await render()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('unavailable')
    await button(wrapper, 'Retry').trigger('click')
    await flushPromises()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
  it.each([{}, [{ token: 'usdt' }], null])(
    'rejects malformed stored assets without clearing them (%j)',
    async (assets) => {
      api.get.mockResolvedValue({ ...config, assets })
      const wrapper = await render()
      expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
      expect(wrapper.find('[role=alert]').exists()).toBe(true)
      expect(api.post).not.toHaveBeenCalled()
      wrapper.unmount()
    }
  )
  it('rejects malformed draft assets before making a configuration write', async () => {
    const wrapper = await render()
    await wrapper.get('textarea').setValue('[{"token":"usdt"}]')
    await button(wrapper, 'Save gateway configuration').trigger('click')
    expect(api.post).not.toHaveBeenCalled()
    expect(wrapper.find('[role=alert]').exists()).toBe(true)
    wrapper.unmount()
  })
  it('keeps editing blocked if the post-save refresh fails', async () => {
    const wrapper = await render()
    api.get.mockRejectedValueOnce(new Error('refresh failed'))
    await button(wrapper, 'Save gateway configuration').trigger('click')
    await flushPromises()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('refresh failed')
    wrapper.unmount()
  })
  it('probes only the public address and ignores a response after it changes', async () => {
    let resolve!: (x: unknown) => void
    api.post.mockReturnValue(
      new Promise((r) => {
        resolve = r
      })
    )
    const wrapper = await render()
    await button(wrapper, 'Load available assets').trigger('click')
    expect(api.post).toHaveBeenCalledWith(
      '/api/option/gmpay/probe',
      { pay_address: 'https://pay.example' },
      { signal: expect.any(AbortSignal) }
    )
    await wrapper.get('input[type=url]').setValue('https://other.example')
    resolve({ supported_assets: [{ secret_marker: 'old' }] })
    await flushPromises()
    expect(wrapper.text()).not.toContain('secret_marker')
    wrapper.unmount()
  })
  it('suppresses completion after leaving the settings panel', async () => {
    let resolve!: (x: unknown) => void
    api.post.mockReturnValue(
      new Promise((r) => {
        resolve = r
      })
    )
    const wrapper = await render()
    await button(wrapper, 'Save gateway configuration').trigger('click')
    wrapper.unmount()
    resolve({})
    await flushPromises()
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(api.get).toHaveBeenCalledOnce()
  })
})
