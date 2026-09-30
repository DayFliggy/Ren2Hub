import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import ModelMetadataDrawer from '../ModelMetadataDrawer.vue'
const mocks = vi.hoisted(() => ({ save: vi.fn(), model: vi.fn() }))
vi.mock('@/api/adminManagement', () => ({
  metadataApi: { saveModel: mocks.save, model: mocks.model },
}))
beforeEach(() => {
  vi.clearAllMocks()
  mocks.save.mockResolvedValue({ id: 42 })
})
function setup() {
  return mount(ModelMetadataDrawer, {
    props: { model: null, initialName: 'test-model', vendors: [] },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
      ],
      stubs: {
        ConsoleModal: { template: '<div><slot /><slot name="footer" /></div>' },
      },
    },
  })
}
describe('metadata save boundary', () => {
  it('saves metadata independently of pricing and emits saved once', async () => {
    const wrapper = setup()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledOnce()
    expect(wrapper.emitted('saved')).toHaveLength(1)
    expect(wrapper.find('[data-pricing-editor]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('preserves metadata form on write failure', async () => {
    mocks.save.mockRejectedValue(new Error('save failed'))
    const wrapper = setup()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('save failed')
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(wrapper.get('input[required]').element).toHaveProperty(
      'value',
      'test-model'
    )
    wrapper.unmount()
  })
})
