import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import ModelMetadataDrawer from '../ModelMetadataDrawer.vue'

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
  mocks.auth.isRoot = true
  mocks.save.mockResolvedValue({ id: 42 })
  mocks.priceSave.mockResolvedValue(undefined)
})
describe('model metadata and pricing save boundary', () => {
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
