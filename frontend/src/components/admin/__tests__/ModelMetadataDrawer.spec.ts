import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import ModelMetadataDrawer from '../ModelMetadataDrawer.vue'
const mocks = vi.hoisted(() => ({ save: vi.fn(), model: vi.fn() }))
vi.mock('@/api/adminManagement', () => ({
  metadataApi: { saveModel: mocks.save, model: mocks.model },
}))
const vendors = [
  {
    id: 7,
    name: 'OpenAI',
    description: '',
    icon: 'OpenAI',
    status: 1,
    created_time: null,
    updated_time: null,
  },
]
beforeEach(() => {
  vi.clearAllMocks()
  mocks.save.mockResolvedValue({ id: 42 })
  mocks.model.mockResolvedValue({
    id: 42,
    model_name: 'gpt-x',
    description: '',
    icon: 'Custom',
    tags: 'chat,fast',
    vendor_id: 7,
    endpoints: '',
    status: 1,
    sync_official: 1,
    name_rule: 0,
    bound_channels: [{ name: 'main', type: 1 }],
    enable_groups: ['default'],
    matched_models: [],
    matched_count: 0,
    quota_types: [],
    created_time: null,
    updated_time: null,
  })
})
function setup(modelId: number | null = null) {
  return mount(ModelMetadataDrawer, {
    props: { modelId, initialName: '  new-model  ', vendors },
    global: {
      plugins: [
        createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
        createRouter({
          history: createMemoryHistory(),
          routes: [
            { path: '/:pathMatch(.*)*', component: { template: '<div />' } },
          ],
        }),
      ],
      stubs: {
        ConsoleModal: { template: '<div><slot /><slot name="footer" /></div>' },
        ConfirmDialog: {
          props: ['open'],
          emits: ['confirm'],
          template:
            '<button v-if="open" data-discard @click="$emit(\'confirm\')" />',
        },
      },
    },
  })
}
const closeButton = (wrapper: ReturnType<typeof setup>) =>
  wrapper.findAll('button').find((button) => button.text() === 'Close')!

describe('model metadata drawer', () => {
  it('protects uncommitted tag text from browser unload and releases the listener', async () => {
    const wrapper = setup(42)
    await flushPromises()
    await wrapper.get('input[aria-label="Tags"]').setValue('still typing')
    const event = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    wrapper.unmount()
    const afterUnmount = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(afterUnmount)
    expect(afterUnmount.defaultPrevented).toBe(false)
  })

  it('saves a trimmed name with typed tags committed on save', async () => {
    const wrapper = setup()
    await wrapper.get('input[aria-label="Tags"]').setValue('chat')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith(
      expect.objectContaining({
        model_name: 'new-model',
        tags: 'chat',
        icon: '',
      })
    )
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })
  it('clears the model icon when switching back to the vendor icon', async () => {
    const wrapper = setup(42)
    await flushPromises()
    expect(mocks.model).toHaveBeenCalledWith(42, expect.any(AbortSignal))
    await wrapper
      .findAll('[role="radio"]')
      .find((radio) => radio.text() === 'Inherit vendor icon')!
      .trigger('click')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.save).toHaveBeenCalledWith(
      expect.objectContaining({ id: 42, icon: '', tags: 'chat,fast' })
    )
    wrapper.unmount()
  })
  it('keeps the form and shows the error when saving fails', async () => {
    mocks.save.mockRejectedValue(new Error('save failed'))
    const wrapper = setup()
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toContain('save failed')
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(wrapper.get('input[required]').element).toHaveProperty(
      'value',
      '  new-model  '
    )
    wrapper.unmount()
  })
  it('asks before discarding edits and closes directly when unchanged', async () => {
    const wrapper = setup(42)
    await flushPromises()
    await closeButton(wrapper).trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    await wrapper.get('textarea').setValue('changed')
    await closeButton(wrapper).trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    await wrapper.get('[data-discard]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(2)
    wrapper.unmount()
  })
})
