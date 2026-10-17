import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TablePagination from '@/components/common/TablePagination.vue'
import { ApiError } from '@/api/types'
import MetadataSyncDialog from '../MetadataSyncDialog.vue'

const mocks = vi.hoisted(() => ({ preview: vi.fn(), sync: vi.fn() }))
// Keep the real conflict predicate so the dialog sees the backend contract.
vi.mock('@/api/adminManagement', async (original) => ({
  ...(await original<typeof import('@/api/adminManagement')>()),
  metadataApi: mocks,
}))
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
  version: 'catalog-v1',
  missing: ['new-model'],
  conflicts: [
    {
      model_name: 'existing-model',
      local_version: 'local-v1',
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
    expect(mocks.sync).toHaveBeenCalledWith('official', 'en', {
      source_version: 'catalog-v1',
      create_models: ['new-model'],
      overwrite: [
        {
          model_name: 'existing-model',
          fields: ['description'],
          local_version: 'local-v1',
        },
      ],
    })
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
    expect(mocks.sync).toHaveBeenCalledWith('official', 'en', {
      source_version: 'catalog-v1',
      create_models: ['new-model'],
      overwrite: [],
    })
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'upstream unavailable'
    )
    expect(wrapper.get('[aria-current="step"]').text()).toContain(
      'Confirm changes'
    )
    expect(wrapper.emitted('saved')).toBeUndefined()
    wrapper.unmount()
  })
  it('discards a stale preview on 409 and offers a fresh one', async () => {
    mocks.sync.mockRejectedValueOnce(
      new ApiError('上游资料已变化，请重新预览', {
        status: 409,
        code: 'METADATA_SOURCE_CHANGED',
      })
    )
    const wrapper = mountDialog()
    await click(wrapper, 'Load metadata preview')
    await click(wrapper, 'Preview selected changes')
    await click(wrapper, 'Review confirmation')
    await click(wrapper, 'Apply 1 model changes')
    expect(wrapper.get('[aria-current="step"]').text()).toContain(
      'Select models'
    )
    const alert = wrapper.get('[role="alert"]')
    expect(alert.text()).toContain('Upstream metadata changed')
    expect(wrapper.text()).not.toContain('new-model')
    expect(wrapper.emitted('saved')).toBeUndefined()
    await alert.get('button').trigger('click')
    await flushPromises()
    expect(mocks.preview).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('new-model')
    wrapper.unmount()
  })
  it('keeps the confirm step for a 409 that is not a catalog change', async () => {
    mocks.sync.mockRejectedValueOnce(
      new ApiError('session changed', {
        status: 409,
        code: 'AUTH_SESSION_CHANGED',
      })
    )
    const wrapper = mountDialog()
    await click(wrapper, 'Load metadata preview')
    await click(wrapper, 'Preview selected changes')
    await click(wrapper, 'Review confirmation')
    await click(wrapper, 'Apply 1 model changes')
    expect(wrapper.get('[aria-current="step"]').text()).toContain(
      'Confirm changes'
    )
    expect(wrapper.get('[role="alert"]').text()).toContain('session changed')
    wrapper.unmount()
  })
  it('names a failed preview and retries it', async () => {
    mocks.preview.mockRejectedValueOnce(new Error('upstream unavailable'))
    const wrapper = mountDialog()
    await click(wrapper, 'Load metadata preview')
    const alert = wrapper.get('[role="alert"]')
    expect(alert.text()).toContain('Failed to preview metadata')
    expect(alert.text()).toContain('upstream unavailable')
    await alert.get('button').trigger('click')
    await flushPromises()
    expect(mocks.preview).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('shows more candidates after the page size grows', async () => {
    mocks.preview.mockResolvedValueOnce({
      missing: Array.from({ length: 25 }, (_, index) => `model-${index}`),
      conflicts: [],
    })
    const wrapper = mountDialog()
    await click(wrapper, 'Load metadata preview')
    expect(wrapper.findAll('tbody tr')).toHaveLength(20)
    wrapper.getComponent(TablePagination).vm.$emit('update:pageSize', 50)
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(25)
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
