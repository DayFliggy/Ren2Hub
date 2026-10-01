import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelManagementView from '@/views/admin/ModelManagementView.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import VendorDrawer from '@/components/admin/VendorDrawer.vue'
import VendorsSection from '@/components/admin/VendorsSection.vue'
import ModelMetadataDrawer from '@/components/admin/ModelMetadataDrawer.vue'

const mocks = vi.hoisted(() => ({
  models: vi.fn(),
  allVendors: vi.fn(),
  setModelStatus: vi.fn(),
  deleteModel: vi.fn(),
  vendors: vi.fn(),
  vendorCounts: vi.fn(),
}))
vi.mock('@/api/adminManagement', () => ({ metadataApi: mocks }))
const rows = [1, 2].map((id) => ({
  id,
  model_name: `model-${id}`,
  description: '',
  icon: '',
  tags: '',
  vendor_id: 1,
  endpoints: '',
  status: 1,
  sync_official: 1,
  name_rule: 0,
  bound_channels: [],
  enable_groups: [],
  matched_models: [],
  matched_count: 0,
  quota_types: [],
  created_time: null,
  updated_time: null,
}))
async function setup(path = '/models/metadata') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      {
        path: '/models/metadata',
        component: ModelManagementView,
        props: { section: 'metadata' },
      },
      {
        path: '/models/vendors',
        component: ModelManagementView,
        props: { section: 'vendors' },
      },
    ],
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(
    { template: '<RouterView />' },
    {
      global: {
        plugins: [
          router,
          createI18n({ legacy: false, locale: 'en', messages: { en: {} } }),
        ],
        stubs: {
          ModelMetadataDrawer: true,
          MetadataSyncDialog: true,
          MissingModelsDialog: true,
          PrefillGroupDialog: true,
          VendorDrawer: true,
          ConfirmDialog: true,
          FieldVisibilityMenu: true,
          PageBreadcrumb: { template: '<div><slot name="action" /></div>' },
          // jsdom renders both layouts; the mobile cards carry the checkboxes.
          DataTable: true,
        },
      },
    }
  )
  await flushPromises()
  return { wrapper, router }
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.models.mockResolvedValue({
    items: rows,
    total: 2,
    vendor_counts: { '1': 2 },
  })
  mocks.allVendors.mockResolvedValue([
    {
      id: 1,
      name: 'Test vendor',
      description: '',
      status: 1,
      icon: '',
      created_time: null,
      updated_time: null,
    },
  ])
  mocks.vendors.mockResolvedValue({
    items: [
      { id: 1, name: 'Test vendor', description: '', icon: '', status: 1 },
      { id: 2, name: 'Other vendor', description: '', icon: '', status: 1 },
    ],
    total: 2,
  })
  mocks.vendorCounts.mockResolvedValue({ '1': 2, '2': 0 })
  mocks.setModelStatus.mockImplementation(async (id: number) => {
    if (id === 2) throw new Error('permission denied')
  })
})
describe('model management sections', () => {
  it('restores NewAPI filter keys from the URL and keeps the hash', async () => {
    const { wrapper, router } = await setup(
      '/models/metadata?filter=model&status=1&page=2#models'
    )
    expect(mocks.models).toHaveBeenCalledWith(
      expect.objectContaining({ keyword: 'model', status: '1', p: 2 }),
      expect.any(AbortSignal)
    )
    const policy = wrapper
      .findAllComponents(FilterSelect)
      .find((select) => select.props('label') === 'Display policy')!
    policy.vm.$emit('update:modelValue', '0')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({
      filter: 'model',
      status: '0',
    })
    expect(router.currentRoute.value.hash).toBe('#models')
    wrapper.unmount()
  })
  it('keeps only failed rows selected after a partial bulk hide', async () => {
    const { wrapper } = await setup()
    await wrapper.get('input[aria-label="Select model-1"]').setValue(true)
    await wrapper.get('input[aria-label="Select model-2"]').setValue(true)
    await wrapper
      .get('button[aria-label="Hide selected models from model square"]')
      .trigger('click')
    await flushPromises()
    expect(mocks.setModelStatus).toHaveBeenCalledWith(1, 0)
    expect(mocks.setModelStatus).toHaveBeenCalledWith(2, 0)
    expect(
      wrapper.get('input[aria-label="Select model-1"]').element
    ).toHaveProperty('checked', false)
    expect(
      wrapper.get('input[aria-label="Select model-2"]').element
    ).toHaveProperty('checked', true)
    expect(wrapper.get('[role="alert"]').text()).toContain(
      'model-2: permission denied'
    )
    wrapper.unmount()
  })
  it('switches to the vendors section without dropping the query', async () => {
    const { wrapper, router } = await setup('/models/metadata?vFilter=open')
    await wrapper.get('[role="tab"][aria-selected="false"]').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/models/vendors')
    expect(router.currentRoute.value.query).toEqual({ vFilter: 'open' })
    expect(wrapper.findComponent(VendorsSection).exists()).toBe(true)
    wrapper.unmount()
  })
  it('opens prefill groups from the deep link and clears it on close', async () => {
    const { wrapper, router } = await setup(
      '/models/metadata?manage=prefill-groups'
    )
    const dialog = wrapper.findComponent({ name: 'PrefillGroupDialog' })
    expect(dialog.props('open')).toBe(true)
    dialog.vm.$emit('close')
    await flushPromises()
    expect(router.currentRoute.value.query.manage).toBeUndefined()
    wrapper.unmount()
  })

  it('refreshes vendor model counts after editing a linked model', async () => {
    const { wrapper } = await setup('/models/vendors')
    expect(wrapper.get('a[href="/models/metadata?vendor=1"]').text()).toContain(
      '2'
    )
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Test vendor')!
      .trigger('click')
    wrapper.getComponent(VendorDrawer).vm.$emit('open-model', { id: 1 })
    await flushPromises()
    const editor = wrapper.getComponent(ModelMetadataDrawer)
    expect(editor.props('modelId')).toBe(1)

    // Moving the linked model changes the count for both affected vendors.
    mocks.vendorCounts.mockResolvedValue({ '1': 1, '2': 1 })
    editor.vm.$emit('saved')
    await flushPromises()

    expect(wrapper.findComponent(ModelMetadataDrawer).exists()).toBe(false)
    expect(mocks.vendorCounts).toHaveBeenCalledTimes(2)
    for (const vendorId of [1, 2]) {
      expect(
        wrapper.get(`a[href="/models/metadata?vendor=${vendorId}"]`).text()
      ).toContain('1')
    }
    wrapper.unmount()
  })
})
