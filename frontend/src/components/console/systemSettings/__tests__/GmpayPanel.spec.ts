import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
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
beforeEach(async () => {
  await setLocale('en')
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
  it.each([
    {
      locale: 'en',
      title: 'GMPay crypto payments',
      retry: 'Reload gateway configuration',
      probe: 'Load available assets',
      save: 'Save gateway configuration',
      name: 'USDT · tron display name',
      icon: 'USDT · tron icon identifier (optional)',
      color: 'USDT · tron color metadata (optional)',
      configured: 'Configured',
      keepSecret: 'Leave blank to keep the existing secret.',
      remove: 'Remove USDT · tron',
      noAssets: 'No payment assets are available.',
      noSelectedAssets: 'No assets selected.',
      addressRequired: 'Enter the gateway address first.',
    },
    {
      locale: 'zh-CN',
      title: 'GMPay 虚拟币支付',
      retry: '重新读取网关配置',
      probe: '探测可用资产',
      save: '保存网关配置',
      name: 'USDT · tron 显示名',
      icon: 'USDT · tron 图标标识（可选）',
      color: 'USDT · tron 颜色元数据（可选）',
      configured: '已配置',
      keepSecret: '留空会保留现有密钥。',
      remove: '移除 USDT · tron',
      noAssets: '暂无可用支付资产',
      noSelectedAssets: '尚未选择支付资产。',
      addressRequired: '请先填写网关地址。',
    },
  ])(
    'resolves every restored $locale state locally without missing translations',
    async (copy) => {
      const missing = vi.fn()
      const warnings = vi.spyOn(console, 'warn').mockImplementation(() => {})
      const localI18n = createI18n({
        legacy: false,
        locale: copy.locale,
        fallbackLocale: 'en',
        messages: { en: {}, 'zh-CN': {} },
        missing,
      })
      api.get.mockRejectedValueOnce(new Error('read unavailable'))
      const wrapper = mount(GmpayPanel, {
        global: { plugins: [localI18n] },
      })
      try {
        expect(wrapper.attributes('aria-label')).toBe(copy.title)
        await flushPromises()
        await button(wrapper, copy.retry).trigger('click')
        await flushPromises()
        expect(wrapper.text()).toContain(copy.configured)
        expect(wrapper.text()).toContain(copy.keepSecret)
        for (const label of [copy.name, copy.icon, copy.color])
          expect(wrapper.text()).toContain(label)

        await wrapper
          .get(`button[aria-label="${copy.remove}"]`)
          .trigger('click')
        expect(wrapper.text()).toContain(copy.noSelectedAssets)
        api.post.mockResolvedValueOnce({ supported_assets: [] })
        await button(wrapper, copy.probe).trigger('click')
        await flushPromises()
        expect(wrapper.text()).toContain(copy.noAssets)
        api.post.mockResolvedValueOnce({
          supported_assets: [
            { network: 'tron', display_name: 'Tron', tokens: ['usdt'] },
          ],
        })
        await button(wrapper, copy.probe).trigger('click')
        await flushPromises()
        await button(wrapper, 'USDT · tron').trigger('click')
        await button(wrapper, copy.save).trigger('click')
        await flushPromises()
        await wrapper.get('input[type=url]').setValue('')
        await button(wrapper, copy.probe).trigger('click')
        expect(wrapper.text()).toContain(copy.addressRequired)
        expect(wrapper.html()).not.toContain('systemSettings.gmpayPanel.')
        expect(missing).not.toHaveBeenCalled()
        expect(
          warnings.mock.calls.filter((call) =>
            String(call[0]).includes('[intlify]')
          )
        ).toEqual([])
      } finally {
        wrapper.unmount()
        warnings.mockRestore()
      }
    }
  )
  it.each([
    {
      locale: 'en',
      label: 'Accept new orders',
      hint: 'Valid payment notifications for existing orders still credit the wallet',
      retired: 'Enable crypto settlement',
      saveLabel: 'Save gateway configuration',
    },
    {
      locale: 'zh-CN',
      label: '开放新订单',
      hint: '已有订单仍会使用下单时保存的商户凭据核验付款通知，核验通过后入账。',
      retired: '启用虚拟币结算',
      saveLabel: '保存网关配置',
    },
  ])(
    'disables only new orders through the native configuration in $locale',
    async ({ locale, label, hint, retired, saveLabel }) => {
      await setLocale(locale)
      const wrapper = await render()
      expect(wrapper.text()).toContain(label)
      expect(wrapper.text()).toContain(hint)
      expect(wrapper.text()).not.toContain(retired)
      await wrapper.get('input[type=checkbox]').setValue(false)
      await button(wrapper, saveLabel).trigger('click')
      await flushPromises()
      expect(api.post).toHaveBeenCalledExactlyOnceWith(
        '/api/option/gmpay/save',
        {
          pay_address: config.pay_address,
          merchant_id: config.merchant_id,
          secret_key: '',
          enabled: false,
          assets: config.assets,
        },
        { signal: expect.any(AbortSignal) }
      )
      wrapper.unmount()
    }
  )
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
    await button(wrapper, 'Reload gateway configuration').trigger('click')
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
    await wrapper.findAll('input[type=text]')[1]!.setValue('   ')
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
  it('restores catalog selection and per-asset editors without the retired pricing controls', async () => {
    const wrapper = await render()
    api.post.mockResolvedValueOnce({
      supported_assets: [
        { network: 'Tron', display_name: 'Tron', tokens: ['USDT', 'USDC'] },
      ],
    })
    await button(wrapper, 'Load available assets').trigger('click')
    await flushPromises()
    expect(wrapper.find('textarea').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('User group price multipliers')
    expect(button(wrapper, 'USDT · tron').attributes('aria-pressed')).toBe(
      'true'
    )
    await button(wrapper, 'USDC · tron').trigger('click')
    expect(button(wrapper, 'USDC · tron').attributes('aria-pressed')).toBe(
      'true'
    )
    const assetInputs = wrapper.findAll('input[type=text]')
    await assetInputs[4]!.setValue('USDC Tron')
    await assetInputs[5]!.setValue('usdc-logo')
    await assetInputs[6]!.setValue('#0088ff')
    await button(wrapper, 'Save gateway configuration').trigger('click')
    await flushPromises()
    expect(api.post).toHaveBeenLastCalledWith(
      '/api/option/gmpay/save',
      expect.objectContaining({
        assets: [
          ...config.assets,
          {
            name: 'USDC Tron',
            token: 'usdc',
            network: 'tron',
            icon: 'usdc-logo',
            color: '#0088ff',
          },
        ],
      }),
      { signal: expect.any(AbortSignal) }
    )
    wrapper.unmount()
  })
  it('preserves an unsaved secret and asset edits when saving fails', async () => {
    const wrapper = await render()
    await wrapper.get('input[type=password]').setValue('draft-secret')
    await wrapper.findAll('input[type=text]')[1]!.setValue('Edited asset')
    api.post.mockRejectedValueOnce(new Error('save failed'))
    await button(wrapper, 'Save gateway configuration').trigger('click')
    await flushPromises()
    expect(wrapper.get('input[type=password]').element).toHaveProperty(
      'value',
      'draft-secret'
    )
    expect(wrapper.findAll('input[type=text]')[1]!.element).toHaveProperty(
      'value',
      'Edited asset'
    )
    expect(wrapper.text()).toContain('save failed')
    expect(wrapper.emitted('saved')).toBeUndefined()
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
