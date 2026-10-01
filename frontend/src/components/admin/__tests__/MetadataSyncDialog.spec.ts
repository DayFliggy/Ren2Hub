import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import MetadataSyncDialog from '../MetadataSyncDialog.vue'

const mocks = vi.hoisted(() => ({ preview: vi.fn(), sync: vi.fn() }))
vi.mock('@/api/adminManagement', () => ({ metadataApi: mocks }))
function mountDialog() {
  return mount(MetadataSyncDialog, {
    props: { open: true },
    global: {
      plugins: [createI18n({ legacy: false, locale: 'en', messages: {} })],
      stubs: {
        ConsoleModal: { template: '<div><slot /><slot name="footer" /></div>' },
      },
    },
  })
}
function button(wrapper: VueWrapper, text: string) {
  const match = wrapper
    .findAll('button')
    .find((item) => item.text().includes(text))
  if (!match) throw new Error(`button "${text}" not found`)
  return match
}
async function click(wrapper: VueWrapper, text: string) {
  await button(wrapper, text).trigger('click')
  await flushPromises()
}
const preview = {
  missing: ['new-model'],
  conflicts: [
    {
      model_name: 'existing-model',
      fields: [
        { field: 'description', local: 'before', upstream: 'after' },
        { field: 'icon', local: 'Old', upstream: 'New' },
        { field: 'vendor', local: 'Local', upstream: 'Upstream' },
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
describe('metadata sync wizard', () => {
  it('submits only the overwrite fields checked in the preview step', async () => {
    const wrapper = mountDialog()
    await click(wrapper, 'Load metadata preview')
    expect(mocks.preview).toHaveBeenCalledWith(
      'official',
      'en',
      expect.any(AbortSignal)
    )
    await wrapper
      .get('input[aria-label="Select existing-model"]')
      .setValue(true)
    await click(wrapper, 'Preview selected changes')
    // `vendor` is localized instead of leaking the raw protocol field name.
    expect(
      wrapper
        .find('input[aria-label="Apply Vendor for existing-model"]')
        .exists()
    ).toBe(true)
    await wrapper
      .get('input[aria-label="Apply Description for existing-model"]')
      .setValue(true)
    await click(wrapper, 'Review confirmation')
    expect(wrapper.text()).toContain('1 models will be created')
    expect(wrapper.text()).toContain('1 fields will be overwritten across 1')
    expect(mocks.sync).not.toHaveBeenCalled()
    await click(wrapper, 'Apply 2 model changes')
    expect(mocks.sync).toHaveBeenCalledWith('official', 'en', [
      { model_name: 'existing-model', fields: ['description'] },
    ])
    expect(wrapper.text()).toContain('Metadata sync completed')
    expect(wrapper.text()).toContain('1 models created, 1 models updated')
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })
  it('shows the sync error and stays on the confirm step', async () => {
    mocks.sync.mockRejectedValueOnce(new Error('upstream unavailable'))
    const wrapper = mountDialog()
    await click(wrapper, 'Load metadata preview')
    await click(wrapper, 'Preview selected changes')
    await click(wrapper, 'Review confirmation')
    await click(wrapper, 'Apply 1 model changes')
    expect(mocks.sync).toHaveBeenCalledWith('official', 'en', [])
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'upstream unavailable'
    )
    expect(wrapper.get('[aria-current="step"]').text()).toContain(
      'Confirm changes'
    )
    expect(wrapper.emitted('saved')).toBeUndefined()
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
    await button(wrapper, 'Load metadata preview').trigger('click')
    const signal = mocks.preview.mock.calls[0][2] as AbortSignal
    await button(wrapper, 'Close').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    await wrapper.setProps({ open: false })
    expect(signal.aborted).toBe(true)
    resolve(preview)
    await flushPromises()
    expect(wrapper.text()).not.toContain('new-model')
    expect(mocks.sync).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
