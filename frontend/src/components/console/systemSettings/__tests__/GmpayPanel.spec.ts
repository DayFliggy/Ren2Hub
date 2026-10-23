vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ refreshStatus: vi.fn().mockResolvedValue(undefined) }),
}))
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import { api } from '@/api/console'
import { httpTransport } from '@/api/httpTransport'
import GmpayPanel from '../GmpayPanel.vue'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const toast = vi.hoisted(() => ({ error: vi.fn(), success: vi.fn() }))
vi.mock('@/composables/useToast', () => ({ useToast: () => toast }))
const config = {
  pay_address: 'https://pay.example',
  merchant_id: '1000',
  has_secret: true,
  enabled: true,
  assets: [
    {
      name: 'USDT Tron',
      token: 'usdt',
      network: 'tron',
      icon: 'usdt',
      color: '#123456',
    },
  ],
}
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
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
describe('GMPay native configuration', () => {
  it('loads the scoped config and sends array assets, enabled, and a blank retained secret', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue(config)
    const post = vi.spyOn(api, 'post').mockResolvedValue(config)
    const wrapper = await render()
    expect(get).toHaveBeenCalledWith('/api/option/gmpay/config', undefined, {
      signal: expect.any(AbortSignal),
    })
    expect(
      wrapper.get<HTMLInputElement>('input[type="password"]').element.value
    ).toBe('')
    const icon = wrapper
      .findAllComponents({ name: 'FormField' })
      .find((field) =>
        String(field.props('label')).includes('USDT · tron icon identifier')
      )!
    await icon.get('input').setValue('coin-usdt')
    await wrapper.get('input[type="checkbox"]').setValue(false)
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Save gateway configuration')!
      .trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith('/api/option/gmpay/save', {
      pay_address: 'https://pay.example',
      merchant_id: '1000',
      secret_key: '',
      enabled: false,
      assets: [{ ...config.assets[0], icon: 'coin-usdt' }],
    })
    expect(post.mock.calls[0]?.[1]).not.toHaveProperty('price')
    expect(post.mock.calls[0]?.[1]).not.toHaveProperty('group_ratio')
    expect(wrapper.text()).toContain('/api/user/gmpay/notify')
    wrapper.unmount()
  })
  it.each(['[]', null, [{}]])(
    'disables editing on invalid native asset catalog %j',
    async (assets) => {
      vi.spyOn(api, 'get').mockResolvedValue({ ...config, assets })
      const post = vi.spyOn(api, 'post')
      const wrapper = await render()
      expect(wrapper.find('[role="alert"]').exists()).toBe(true)
      expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
      expect(post).not.toHaveBeenCalled()
      wrapper.unmount()
    }
  )
  it('probes only the public gateway address and preserves configured assets', async () => {
    vi.spyOn(api, 'get').mockResolvedValue(config)
    const post = vi.spyOn(api, 'post').mockResolvedValue({
      supported_assets: [
        { network: 'ethereum', display_name: 'Ethereum', tokens: ['USDC'] },
      ],
    })
    const wrapper = await render()
    await wrapper
      .findAll('button')
      .find(
        (button) =>
          button.text() === i18n.global.t('systemSettings.gmpayPanel.probe')
      )!
      .trigger('click')
    await flushPromises()
    expect(post).toHaveBeenCalledWith(
      '/api/option/gmpay/probe',
      { pay_address: config.pay_address },
      { signal: expect.any(AbortSignal) }
    )
    expect(wrapper.text()).toContain('USDC')
    expect(wrapper.text()).toContain('USDT')
    wrapper.unmount()
  })
  it('shows the translated backend reason when saving fails', async () => {
    // The transport returns the Go SaveGmpayConfig envelope unchanged (no data).
    const request = vi
      .spyOn(httpTransport, 'request')
      .mockResolvedValueOnce({ success: true, message: '', data: config })
      .mockResolvedValueOnce({
        success: false,
        message: 'Duplicate GMPay asset',
        message_key: 'Duplicate GMPay asset',
      } as never)
    const wrapper = await render()
    await setLocale('zh-CN')
    try {
      await wrapper
        .findAll('button')
        .find(
          (button) =>
            button.text() === i18n.global.t('systemSettings.gmpayPanel.save')
        )!
        .trigger('click')
      await flushPromises()
    } finally {
      await setLocale('en')
    }
    expect(request).toHaveBeenLastCalledWith(
      'POST',
      '/api/option/gmpay/save',
      expect.anything()
    )
    expect(toast.error).toHaveBeenCalledWith('GMPay 资产重复')
    expect(toast.success).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('reloads after a failed read without silently replacing the stored catalog', async () => {
    vi.spyOn(api, 'get')
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(config)
    const wrapper = await render()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Reload gateway configuration')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeUndefined()
    expect(
      wrapper.get<HTMLInputElement>('input[type="url"]').element.value
    ).toBe(config.pay_address)
    wrapper.unmount()
  })
})
