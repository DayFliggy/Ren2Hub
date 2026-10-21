import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import TablePagination from '@/components/common/TablePagination.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import { ApiError } from '@/api/types'
import { parseSyncPreview } from '@/api/adminManagement'
import MetadataSyncDialog from '../MetadataSyncDialog.vue'

const mocks = vi.hoisted(() => ({ preview: vi.fn(), sync: vi.fn() }))
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
const preview = parseSyncPreview({
  source: { locale: 'en', version: 'catalog-v1' },
  candidates: [
    {
      model_name: 'new-model',
      kind: 'create',
      scope: 'site',
      record_version: 'new-v1',
      vendor_to_create: 'Provider',
      fields: [{ field: 'description', local: null, upstream: 'New metadata' }],
    },
    {
      model_name: 'existing-model',
      kind: 'update',
      scope: 'site',
      record_version: 'local-v1',
      fields: [
        { field: 'description', local: 'before', upstream: 'after' },
        { field: 'icon', local: 'Old', upstream: 'New' },
        { field: 'vendor', local: 'Local', upstream: 'Upstream' },
      ],
    },
    ...['blocked', 'unchanged', 'missing_upstream', 'missing_vendor'].map(
      (kind) => ({
        model_name: kind,
        kind,
        scope: 'site',
        record_version: '',
        fields: [],
      })
    ),
  ],
})
const create = {
  model_name: 'new-model',
  record_version: 'new-v1',
  create: true,
  fields: ['description'],
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.preview.mockResolvedValue(preview)
  mocks.sync.mockResolvedValue({
    created_models: ['new-model'],
    updated_models: [{ model_name: 'existing-model', fields: ['description'] }],
    created_vendors: ['Provider'],
  })
})
async function selectNew(wrapper: VueWrapper) {
  await wrapper.get('input[aria-label="Select new-model"]').setValue(true)
}
async function confirmNew(wrapper: VueWrapper) {
  await click(wrapper, 'Load metadata preview')
  await selectNew(wrapper)
  await click(wrapper, 'Preview selected changes')
  await click(wrapper, 'Review confirmation')
}
describe('native metadata sync wizard', () => {
  it('requires explicit model selection and sends only selected native records and fields', async () => {
    const wrapper = mountDialog()
    await click(wrapper, 'Load metadata preview')
    expect(mocks.preview).toHaveBeenCalledWith('en', expect.any(AbortSignal))
    expect(
      button(wrapper, 'Preview selected changes').attributes('disabled')
    ).toBeDefined()
    await selectNew(wrapper)
    await wrapper
      .get('input[aria-label="Select existing-model"]')
      .setValue(true)
    for (const kind of [
      'blocked',
      'unchanged',
      'missing_upstream',
      'missing_vendor',
    ])
      expect(wrapper.find(`input[aria-label="Select ${kind}"]`).exists()).toBe(
        false
      )
    expect(wrapper.text()).toContain('Metadata sync disabled')
    await click(wrapper, 'Preview selected changes')
    expect(wrapper.text()).toContain('New metadata')
    expect(
      wrapper
        .get('input[aria-label="Apply Description for new-model"]')
        .attributes('disabled')
    ).toBeDefined()
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
    expect(wrapper.text()).toContain('Vendors to create: Provider')
    expect(mocks.sync).not.toHaveBeenCalled()
    await click(wrapper, 'Apply 2 model changes')
    expect(mocks.sync).toHaveBeenCalledWith('en', {
      source_version: 'catalog-v1',
      selections: [
        create,
        {
          model_name: 'existing-model',
          fields: ['description'],
          record_version: 'local-v1',
          create: false,
        },
      ],
    })
    expect(wrapper.text()).toContain(
      '1 models created, 1 models updated, 1 vendors created'
    )
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })
  it.each([
    'Upstream metadata changed; preview again',
    'metadata changed; preview again before applying: new-model',
  ])(
    'shows native 409 and reloads versions before retry: %s',
    async (message) => {
      mocks.sync.mockRejectedValueOnce(new ApiError(message, { status: 409 }))
      const wrapper = mountDialog()
      await confirmNew(wrapper)
      await click(wrapper, 'Apply 1 model changes')
      expect(wrapper.get('[role="alert"]').text()).toContain(message)
      expect(wrapper.emitted('saved')).toBeUndefined()
      await click(wrapper, 'Preview again')
      expect(mocks.preview).toHaveBeenCalledTimes(2)
      expect(
        button(wrapper, 'Preview selected changes').attributes('disabled')
      ).toBeDefined()
      expect(wrapper.text()).toContain('0 selected models')
      wrapper.unmount()
    }
  )
  it('preserves confirmation and its plan when the server is unavailable', async () => {
    mocks.sync.mockRejectedValueOnce(new Error('upstream unavailable'))
    const wrapper = mountDialog()
    await confirmNew(wrapper)
    await click(wrapper, 'Apply 1 model changes')
    expect(mocks.sync).toHaveBeenCalledWith('en', {
      source_version: 'catalog-v1',
      selections: [create],
    })
    expect(wrapper.get('[aria-current="step"]').text()).toContain(
      'Confirm changes'
    )
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'upstream unavailable'
    )
    expect(wrapper.emitted('saved')).toBeUndefined()
    wrapper.unmount()
  })
  it('can clear all model selections and requires fields for an update-only plan', async () => {
    const wrapper = mountDialog()
    await click(wrapper, 'Load metadata preview')
    await click(wrapper, 'Select all syncable models')
    expect(wrapper.text()).toContain('2 selected models')
    await click(wrapper, 'Clear selection')
    expect(wrapper.text()).toContain('0 selected models')
    await wrapper
      .get('input[aria-label="Select existing-model"]')
      .setValue(true)
    await click(wrapper, 'Preview selected changes')
    expect(
      button(wrapper, 'Review confirmation').attributes('disabled')
    ).toBeDefined()
    await click(wrapper, 'Select all changed fields')
    expect(
      button(wrapper, 'Review confirmation').attributes('disabled')
    ).toBeUndefined()
    await click(wrapper, 'Clear field selection')
    expect(
      button(wrapper, 'Review confirmation').attributes('disabled')
    ).toBeDefined()
    expect(mocks.sync).not.toHaveBeenCalled()
    wrapper.unmount()
  })
  it('keeps selected records outside scope and search without selecting skipped candidates', async () => {
    mocks.preview.mockResolvedValueOnce({
      ...preview,
      candidates: [
        ...preview.candidates,
        {
          ...preview.candidates[0],
          model_name: 'catalog-only',
          scope: 'catalog',
        },
      ],
    })
    const wrapper = mountDialog()
    await click(wrapper, 'Load metadata preview')
    expect(wrapper.text()).not.toContain('catalog-only')
    await selectNew(wrapper)
    const scope = wrapper
      .findAllComponents(FilterSelect)
      .find((item) => item.props('label') === 'Sync scope')!
    scope.vm.$emit('update:modelValue', 'all')
    await flushPromises()
    await wrapper.get('input[aria-label="Select catalog-only"]').setValue(true)
    scope.vm.$emit('update:modelValue', 'site')
    await flushPromises()
    expect(wrapper.text()).toContain('1 selected outside the current filters')
    await click(wrapper, 'Preview selected changes')
    await click(wrapper, 'Review confirmation')
    await click(wrapper, 'Apply 2 model changes')
    expect(
      mocks.sync.mock.calls[0][1].selections.map(
        (item: { model_name: string }) => item.model_name
      )
    ).toEqual(['new-model', 'catalog-only'])
    wrapper.unmount()
  })
  it('shows more candidates after the page size grows without selecting them', async () => {
    mocks.preview.mockResolvedValueOnce({
      ...preview,
      candidates: Array.from({ length: 25 }, (_, index) => ({
        ...preview.candidates[0],
        model_name: `model-${index}`,
      })),
    })
    const wrapper = mountDialog()
    await click(wrapper, 'Load metadata preview')
    expect(wrapper.findAll('tbody tr')).toHaveLength(20)
    wrapper.getComponent(TablePagination).vm.$emit('update:pageSize', 50)
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(25)
    expect(wrapper.text()).toContain('0 selected models')
    wrapper.unmount()
  })
  it('names a failed preview and retries it', async () => {
    mocks.preview.mockRejectedValueOnce(new Error('upstream unavailable'))
    const wrapper = mountDialog()
    await click(wrapper, 'Load metadata preview')
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'Failed to preview metadata'
    )
    await click(wrapper, 'Retry')
    expect(mocks.preview).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    wrapper.unmount()
  })
  it('aborts an in-flight preview on close and never writes on cancel', async () => {
    let resolve!: (value: typeof preview) => void
    mocks.preview.mockImplementationOnce(
      () =>
        new Promise((done) => {
          resolve = done
        })
    )
    const wrapper = mountDialog()
    await button(wrapper, 'Load metadata preview').trigger('click')
    const signal = mocks.preview.mock.calls[0][1] as AbortSignal
    await button(wrapper, 'Close').trigger('click')
    await wrapper.setProps({ open: false })
    expect(signal.aborted).toBe(true)
    resolve(preview)
    await flushPromises()
    expect(wrapper.text()).not.toContain('new-model')
    expect(mocks.sync).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
