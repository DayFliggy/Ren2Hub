import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
const mocks = vi.hoisted(() => ({
  load: vi.fn(),
  rawValue: vi.fn(),
  saveOptions: vi.fn(),
  payment: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
}))
vi.mock('@/api/topup', () => ({ paymentRequest: mocks.payment }))
vi.mock('@/composables/useSystemSettings', () => ({
  useSystemSettings: () => mocks,
}))
vi.mock('@/composables/useToast', () => ({ useToast: () => mocks }))
import WaffoPancakePanel from '../WaffoPancakePanel.vue'
beforeEach(() => {
  vi.clearAllMocks()
  mocks.load.mockResolvedValue(true)
  mocks.saveOptions.mockResolvedValue(true)
  mocks.rawValue.mockImplementation(
    (key, fallback) =>
      (
        ({
          WaffoPancakeMerchantID: 'merchant',
          WaffoPancakeReturnURL: 'https://site.example/wallet',
          WaffoPancakeStoreID: 'store',
          WaffoPancakeProductID: 'product',
        }) as Record<string, string>
      )[key as string] ?? fallback
  )
  mocks.payment.mockResolvedValue({
    data: { store_id: 'store', product_id: 'product' },
  })
})
async function render() {
  const wrapper = mount(WaffoPancakePanel, {
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
    },
  })
  await flushPromises()
  return wrapper
}
it('keeps an undisclosed existing private key and saves native price options separately', async () => {
  const wrapper = await render()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'systemSettings.waffoPancake.save')!
    .trigger('click')
  await flushPromises()
  expect(mocks.payment).toHaveBeenCalledWith('/api/option/waffo-pancake/save', {
    merchant_id: 'merchant',
    private_key: '',
    return_url: 'https://site.example/wallet',
    store_id: 'store',
    product_id: 'product',
  })
  expect(mocks.saveOptions).toHaveBeenCalledWith({
    WaffoPancakeUnitPrice: 1,
    WaffoPancakeMinTopUp: 1,
  })
  expect(wrapper.emitted('saved')).toHaveLength(1)
  wrapper.unmount()
})
it('does not report all settings saved when the subsequent native option writes fail', async () => {
  mocks.saveOptions.mockResolvedValue(false)
  const wrapper = await render()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'systemSettings.waffoPancake.save')!
    .trigger('click')
  await flushPromises()
  expect(wrapper.emitted('saved')).toBeUndefined()
  expect(mocks.success).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('loads the catalog through the native GET envelope after saving settings', async () => {
  mocks.payment.mockResolvedValueOnce({ data: {} }).mockResolvedValueOnce({
    data: { stores: [] },
  })
  const wrapper = await render()
  await wrapper
    .findAll('button')
    .find(
      (button) => button.text() === 'systemSettings.waffoPancake.saveAndLoad'
    )!
    .trigger('click')
  await flushPromises()
  expect(mocks.payment).toHaveBeenLastCalledWith(
    '/api/option/waffo-pancake/catalog',
    undefined,
    undefined,
    'GET'
  )
  expect(mocks.error).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('uses persisted credentials for pairing without sending a merchant ID with a blank private key', async () => {
  const wrapper = await render()
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'systemSettings.waffoPancake.createPair')!
    .trigger('click')
  await flushPromises()
  expect(mocks.payment).toHaveBeenCalledWith('/api/option/waffo-pancake/pair', {
    return_url: 'https://site.example/wallet',
  })
  wrapper.unmount()
})
