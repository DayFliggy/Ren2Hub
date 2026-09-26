import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import ModelMetadataDrawer from '../ModelMetadataDrawer.vue'
import { modelPricingApi } from '@/api/modelPricing'
import { automaticPricingApi } from '@/api/automaticPricing'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

const mocks = vi.hoisted(() => ({
  save: vi.fn(),
  model: vi.fn(),
  priceValidate: vi.fn(),
  priceSave: vi.fn(),
  auth: { isRoot: true },
}))
vi.mock('@/api/adminManagement', () => ({
  metadataApi: { saveModel: mocks.save, model: mocks.model },
}))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
const PricingStub = defineComponent({
  setup(_, { expose }) {
    expose({ validate: mocks.priceValidate, save: mocks.priceSave })
    return {}
  },
  template: '<div data-pricing-editor />',
})
const ModalStub = { template: '<div><slot /><slot name="footer" /></div>' }
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
function mountDrawer() {
  return mount(ModelMetadataDrawer, {
    props: { model: null, initialName: 'test-model', vendors: [] },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
      ],
      stubs: { ConsoleModal: ModalStub, ModelPricingEditor: PricingStub },
    },
  })
}
beforeEach(() => {
  vi.clearAllMocks()
  vi.spyOn(automaticPricingApi, 'status').mockResolvedValue({
    config: { enabled: true, check_interval_minutes: 30 },
    sources: [],
    origins: [],
  })
  mocks.auth.isRoot = true
  mocks.save.mockResolvedValue({ id: 42 })
  mocks.priceSave.mockResolvedValue(undefined)
})
describe('model metadata and pricing save boundary', () => {
  it.each(['', 'test-model'])(
    'uses the entered model for schema and publication from %j',
    async (initialName) => {
      vi.useFakeTimers()
      vi.spyOn(modelPricingApi, 'load').mockResolvedValue([])
      const schema = vi
        .spyOn(modelPricingApi, 'taskSchema')
        .mockImplementation(async (model) => ({
          model,
          plugin_key: '',
          usage_schema: null,
          manifest_schema: null,
          review_required: false,
          plugins: [],
        }))
      const publish = vi
        .spyOn(modelPricingApi, 'publish')
        .mockImplementation(async (price) => ({ ...price, id: 1 }))
      const wrapper = mount(ModelMetadataDrawer, {
        props: { model: null, initialName, vendors: [] },
        global: { plugins: [i18n], stubs: { ConsoleModal: ModalStub } },
      })
      try {
        await flushPromises()
        await wrapper.get('input[required]').setValue('new-model')
        for (const input of wrapper.findAll('input[inputmode=decimal]'))
          await input.setValue('1')
        await vi.advanceTimersByTimeAsync(350)
        await flushPromises()
        await wrapper.get('form').trigger('submit')
        await flushPromises()
        expect(schema).toHaveBeenCalledWith(
          'new-model',
          undefined,
          expect.any(AbortSignal)
        )
        expect(publish).toHaveBeenCalledWith(
          expect.objectContaining({ model: 'new-model' }),
          0,
          undefined
        )
        expect(wrapper.emitted('saved')).toHaveLength(1)
      } finally {
        wrapper.unmount()
        vi.useRealTimers()
      }
    }
  )
  it('retains the created model and retries pricing without creating metadata again', async () => {
    mocks.priceSave.mockRejectedValueOnce(new Error('price write failed'))
    const wrapper = mountDrawer()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain(
      'Model metadata was saved, but pricing failed'
    )
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledTimes(1)
    expect(mocks.priceSave).toHaveBeenCalledTimes(2)
    expect(mocks.priceSave).toHaveBeenLastCalledWith('test-model')
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })
  it('does not mount or validate the Root pricing editor for Admin users', async () => {
    mocks.auth.isRoot = false
    const wrapper = mountDrawer()
    expect(wrapper.find('[data-pricing-editor]').exists()).toBe(false)
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.priceValidate).not.toHaveBeenCalled()
    expect(mocks.priceSave).not.toHaveBeenCalled()
    expect(mocks.save).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
  it('does not create metadata when pricing validation fails', async () => {
    mocks.priceValidate.mockImplementationOnce(() => {
      throw new Error('invalid pricing')
    })
    const wrapper = mountDrawer()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('invalid pricing')
    wrapper.unmount()
  })
})
