import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import ModelPricingEditor from '../ModelPricingEditor.vue'
import { modelPricingApi, type PriceVersion } from '@/api/modelPricing'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const price: PriceVersion = {
  id: 3,
  model: 'test',
  mode: 'unit_price',
  prices: [{ quantity: 'p', unit: 'per_million_tokens', price: '1' }],
  created_at: 1,
  hash: 'hash',
}
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(() => vi.restoreAllMocks())
function setup() {
  vi.spyOn(modelPricingApi, 'load').mockResolvedValue([price])
  const publish = vi
    .spyOn(modelPricingApi, 'publish')
    .mockResolvedValue({ ...price, id: 4 })
  const wrapper = mount(ModelPricingEditor, {
    props: { modelName: 'test' },
    global: { plugins: [i18n] },
  })
  return { wrapper, publish }
}
describe('model price editor', () => {
  it('publishes exact decimal text and leaves the loaded immutable version intact', async () => {
    const { wrapper, publish } = setup()
    await flushPromises()
    await wrapper
      .get('input[inputmode=decimal]')
      .setValue('1.000000000000000001')
    await wrapper.vm.save('test')
    expect(publish).toHaveBeenCalledWith(
      expect.objectContaining({
        prices: [
          {
            quantity: 'p',
            unit: 'per_million_tokens',
            price: '1.000000000000000001',
          },
        ],
      }),
      3
    )
    expect(price.prices[0]?.price).toBe('1')
    wrapper.unmount()
  })
  it('rejects concurrent target changes before publishing', async () => {
    const { wrapper, publish } = setup()
    await flushPromises()
    await wrapper.get('input[inputmode=decimal]').setValue('2')
    vi.mocked(modelPricingApi.load).mockResolvedValue([{ ...price, id: 5 }])
    await expect(wrapper.vm.validate('test')).rejects.toThrow(
      'changed elsewhere'
    )
    expect(publish).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('makes no price write for unchanged metadata and aborts unloaded reads', async () => {
    const { wrapper, publish } = setup()
    await flushPromises()
    await wrapper.vm.save('test')
    expect(publish).not.toHaveBeenCalled()
    wrapper.unmount()
    const load = vi
      .mocked(modelPricingApi.load)
      .mockImplementation(() => new Promise(() => {}))
    const pending = mount(ModelPricingEditor, {
      props: { modelName: 'test' },
      global: { plugins: [i18n] },
    })
    await expect(pending.vm.validate()).rejects.toThrow(
      'not loaded successfully'
    )
    const signal = load.mock.calls.at(-1)![0]!
    pending.unmount()
    expect(signal.aborted).toBe(true)
  })
})
