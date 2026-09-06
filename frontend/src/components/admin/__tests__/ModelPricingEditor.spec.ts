import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ModelPricingEditor from '../ModelPricingEditor.vue'
import {
  MODEL_PRICE_KEYS,
  modelPricingApi,
  type ModelPriceMaps,
} from '@/api/modelPricing'

function setup() {
  const maps: ModelPriceMaps = Object.fromEntries(
    MODEL_PRICE_KEYS.map((key) => [key, {}])
  )
  maps.ModelRatio = { test: 2 }
  maps.CompletionRatio = { test: 3 }
  vi.spyOn(modelPricingApi, 'load').mockResolvedValue(maps)
  const save = vi.spyOn(modelPricingApi, 'save').mockResolvedValue(maps)
  const wrapper = mount(ModelPricingEditor, {
    props: { modelName: 'test' },
    global: { plugins: [createI18n({ legacy: false, locale: 'en' })] },
  })
  return { wrapper, save }
}
afterEach(() => vi.restoreAllMocks())
describe('model price form', () => {
  it('preserves numeric input as text and converts per-million token prices', async () => {
    const { wrapper, save } = setup()
    await flushPromises()
    await wrapper.get('input[type=number]').setValue('1.5')
    await wrapper.vm.validate('test')
    await wrapper.vm.save('test')
    expect(save.mock.calls[0]![0].fields.ModelRatio).toBe('1.5')
    await wrapper.get('input[type=radio][value=price]').setValue()
    await wrapper.get('input[type=number]').setValue('4')
    await wrapper.findAll('input[type=number]')[1]!.setValue('12')
    await wrapper.vm.save('test')
    expect(save.mock.calls[1]![0].fields).toMatchObject({
      ModelRatio: '2',
      CompletionRatio: '3',
    })
    wrapper.unmount()
  })
  it('rejects a target price conflict before metadata is submitted', async () => {
    const { wrapper } = setup()
    await flushPromises()
    vi.mocked(modelPricingApi.load).mockResolvedValue({
      ModelPrice: { renamed: 10 },
      ModelRatio: { test: 2 },
      CompletionRatio: { test: 3 },
    })
    await expect(wrapper.vm.validate('renamed')).rejects.toThrow(
      'target model name already has pricing'
    )
    wrapper.unmount()
  })
  it('aborts pending reads on disposal and cannot save an unloaded map', async () => {
    const load = vi
      .spyOn(modelPricingApi, 'load')
      .mockImplementation(() => new Promise(() => {}))
    const wrapper = mount(ModelPricingEditor, {
      props: { modelName: 'test' },
      global: { plugins: [createI18n({ legacy: false, locale: 'en' })] },
    })
    await expect(wrapper.vm.validate()).rejects.toThrow(
      'not loaded successfully'
    )
    const signal = load.mock.calls[0]![0]!
    wrapper.unmount()
    expect(signal.aborted).toBe(true)
  })
})
