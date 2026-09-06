import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MetadataSyncDialog from '../MetadataSyncDialog.vue'

const mocks = vi.hoisted(() => ({ preview: vi.fn(), sync: vi.fn() }))
vi.mock('@/api/adminManagement', () => ({ metadataApi: mocks }))
function mountDialog() {
  return mount(MetadataSyncDialog, {
    props: { open: true },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          messages: {
            en: {
              common: {
                pageSize: '{size}',
                pageInfo: '{total}',
                itemsPerPage: 'Page size',
              },
            },
          },
        }),
      ],
      stubs: {
        ConsoleModal: { template: '<div><slot /><slot name="footer" /></div>' },
      },
    },
  })
}
const preview = {
  missing: ['new-model'],
  conflicts: [
    {
      model_name: 'existing-model',
      fields: [
        { field: 'description', local: 'before', upstream: 'after' },
        { field: 'icon', local: 'Old', upstream: 'New' },
      ],
    },
  ],
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.preview.mockResolvedValue(preview)
  mocks.sync.mockResolvedValue({
    created_models: 1,
    updated_models: 1,
    created_vendors: 0,
    skipped_models: [],
  })
})
describe('metadata sync workflow', () => {
  it('keeps config source disabled and submits only explicitly selected fields', async () => {
    const wrapper = mountDialog()
    expect(
      wrapper.get('option[value="config"]').attributes('disabled')
    ).toBeDefined()
    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('Get sync preview'))!
      .trigger('click')
    await flushPromises()
    expect(mocks.sync).not.toHaveBeenCalled()
    await wrapper
      .get('input[aria-label="Overwrite field existing-model description"]')
      .setValue(true)
    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('Confirm sync'))!
      .trigger('click')
    await flushPromises()
    expect(mocks.sync).toHaveBeenCalledWith('official', 'en', [
      { model_name: 'existing-model', fields: ['description'] },
    ])
    expect(wrapper.text()).toContain('Created 1 models, updated 1 models')
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })
  it('aborts an in-flight preview on close and never writes on cancel', async () => {
    let resolve: (value: typeof preview) => void = () => {}
    mocks.preview.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done
        })
    )
    const wrapper = mountDialog()
    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('Get sync preview'))!
      .trigger('click')
    const signal = mocks.preview.mock.calls[0][2] as AbortSignal
    await wrapper.setProps({ open: false })
    expect(signal.aborted).toBe(true)
    resolve(preview)
    await flushPromises()
    expect(wrapper.text()).not.toContain('new-model')
    expect(mocks.sync).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
