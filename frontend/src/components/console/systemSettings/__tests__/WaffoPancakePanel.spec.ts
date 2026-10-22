import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, expect, it, vi, type MockInstance } from 'vitest'
import { createI18n } from 'vue-i18n'
import { httpTransport } from '@/api/httpTransport'
const mocks = vi.hoisted(() => ({
  load: vi.fn(),
  rawValue: vi.fn(),
  saveOptions: vi.fn(),
  success: vi.fn(),
  error: vi.fn(),
}))
// The panel goes through the real paymentRequest; only the transport answers
// with the envelopes controller/topup_waffo_pancake.go writes.
let request: MockInstance<typeof httpTransport.request>
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
  request = vi.spyOn(httpTransport, 'request').mockResolvedValue({
    message: 'success',
    data: { store_id: 'store', product_id: 'product' },
  } as never)
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
          messages: {
            en: {
              systemSettings: {
                waffoPancake: {
                  orphanStore: 'orphan {id}: {reason}',
                  pairFailed: 'pair failed: {reason}',
                },
              },
            },
          },
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
  expect(request).toHaveBeenCalledWith(
    'POST',
    '/api/option/waffo-pancake/save',
    {
      data: {
        merchant_id: 'merchant',
        private_key: '',
        return_url: 'https://site.example/wallet',
        store_id: 'store',
        product_id: 'product',
      },
      signal: undefined,
    }
  )
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
  request
    .mockResolvedValueOnce({ message: 'success', data: {} } as never)
    .mockResolvedValueOnce({
      message: 'success',
      data: { stores: [] },
    } as never)
  const wrapper = await render()
  await wrapper
    .findAll('button')
    .find(
      (button) => button.text() === 'systemSettings.waffoPancake.saveAndLoad'
    )!
    .trigger('click')
  await flushPromises()
  expect(request).toHaveBeenLastCalledWith(
    'GET',
    '/api/option/waffo-pancake/catalog',
    { data: undefined, signal: undefined }
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
  expect(request).toHaveBeenCalledWith(
    'POST',
    '/api/option/waffo-pancake/pair',
    { data: { return_url: 'https://site.example/wallet' }, signal: undefined }
  )
  wrapper.unmount()
})

async function createPair(wrapper: Awaited<ReturnType<typeof render>>) {
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'systemSettings.waffoPancake.createPair')!
    .trigger('click')
  await flushPromises()
}

it('keeps the orphan store and shows its ID with the failure reason', async () => {
  const wrapper = await render()
  request.mockResolvedValueOnce({
    message: 'error',
    data: {
      error: 'product create failed',
      store_id: 'store-new',
      store_name: 'Ren2Hub',
      orphan_store: true,
    },
  } as never)
  await createPair(wrapper)
  expect(mocks.error).toHaveBeenCalledWith(
    'orphan store-new: product create failed'
  )
  await wrapper
    .findAll('button')
    .find((b) => b.text() === 'systemSettings.waffoPancake.save')!
    .trigger('click')
  await flushPromises()
  expect(request).toHaveBeenLastCalledWith(
    'POST',
    '/api/option/waffo-pancake/save',
    expect.objectContaining({
      data: expect.objectContaining({ store_id: 'store-new', product_id: '' }),
    })
  )
  wrapper.unmount()
})

it('shows the reason when the pair fails before a store exists', async () => {
  const wrapper = await render()
  request.mockResolvedValueOnce({
    message: 'error',
    data: { error: 'store create failed' },
  } as never)
  await createPair(wrapper)
  expect(mocks.error).toHaveBeenCalledWith('pair failed: store create failed')
  wrapper.unmount()
})
