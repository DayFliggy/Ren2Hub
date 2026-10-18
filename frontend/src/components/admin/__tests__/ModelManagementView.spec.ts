import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelManagementView from '@/views/admin/ModelManagementView.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import VendorDrawer from '@/components/admin/VendorDrawer.vue'
import VendorsSection from '@/components/admin/VendorsSection.vue'
import ModelMetadataDrawer from '@/components/admin/ModelMetadataDrawer.vue'
import ModelsSection from '@/components/admin/ModelsSection.vue'
import ActionMenu from '@/components/common/ActionMenu.vue'

const mocks = vi.hoisted(() => ({
  models: vi.fn(),
  allVendors: vi.fn(),
  setModelStatus: vi.fn(),
  deleteModel: vi.fn(),
  vendors: vi.fn(),
  vendorCounts: vi.fn(),
  model: vi.fn(),
  saveModel: vi.fn(),
}))
vi.mock('@/api/adminManagement', () => ({ metadataApi: mocks }))
// This suite tests workspace navigation; keep its lazy pricing import isolated
// from API initialization after the navigation wrapper has unmounted.
vi.mock('@/views/admin/ModelPricingView.vue', () => ({
  default: { name: 'ModelPricingView', template: '<div>Pricing view</div>' },
}))
const authState = vi.hoisted(() => ({ isRoot: true }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState,
}))
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
  matched_models: [],
  matched_count: 0,
  quota_types: [],
  created_time: null,
  updated_time: null,
}))
async function setup(path = '/models/metadata', realModelDrawer = false) {
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
      {
        path: '/models/pricing',
        component: ModelManagementView,
        props: { section: 'pricing' },
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
          ModelMetadataDrawer: !realModelDrawer,
          ConsoleModal: {
            template: '<div><slot /><slot name="footer" /></div>',
          },
          MetadataSyncDialog: true,
          MissingModelsDialog: true,
          PrefillGroupDialog: true,
          ModelPricingView: true,
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
  authState.isRoot = true
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
  mocks.model.mockResolvedValue(rows[0])
  mocks.saveModel.mockResolvedValue(rows[0])
  mocks.setModelStatus.mockImplementation(async (id: number) => {
    if (id === 2) throw new Error('permission denied')
  })
})
describe('model management sections', () => {
  it.each(['metadata', 'vendors'] as const)(
    'discards the model draft only after a confirmed section change from %s',
    async (section) => {
      const originalPath = `/models/${section}`
      const destination = `/models/${section === 'metadata' ? 'vendors' : 'metadata'}`
      const { wrapper, router } = await setup(originalPath, true)
      const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
      try {
        if (section === 'metadata')
          wrapper.getComponent(ModelsSection).vm.$emit('edit', 1)
        else
          wrapper.getComponent(VendorsSection).vm.$emit('open-model', { id: 1 })
        await flushPromises()
        await wrapper.get('#model-metadata-name').setValue('unsaved-model')

        await router.push(destination)
        await flushPromises()
        expect(router.currentRoute.value.path).toBe(originalPath)
        expect(wrapper.get('#model-metadata-name').element).toHaveProperty(
          'value',
          'unsaved-model'
        )
        expect(confirm).toHaveBeenCalledTimes(1)

        confirm.mockReturnValue(true)
        await router.push(destination)
        await flushPromises()
        expect(router.currentRoute.value.path).toBe(destination)
        expect(wrapper.findComponent(ModelMetadataDrawer).exists()).toBe(false)
        expect(confirm).toHaveBeenCalledTimes(2)

        await router.push(originalPath)
        await flushPromises()
        expect(confirm).toHaveBeenCalledTimes(2)
        if (section === 'metadata')
          wrapper.getComponent(ModelsSection).vm.$emit('edit', 1)
        else
          wrapper.getComponent(VendorsSection).vm.$emit('open-model', { id: 1 })
        await flushPromises()
        expect(wrapper.get('#model-metadata-name').element).toHaveProperty(
          'value',
          'model-1'
        )
        expect(mocks.model).toHaveBeenCalledTimes(2)
        expect(mocks.saveModel).not.toHaveBeenCalled()
      } finally {
        wrapper.unmount()
        confirm.mockRestore()
      }
    }
  )

  it('keeps the model drawer and section while its save is pending', async () => {
    const { wrapper, router } = await setup('/models/metadata', true)
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    let finishSave!: (value: (typeof rows)[number]) => void
    mocks.saveModel.mockReturnValueOnce(
      new Promise((resolve) => {
        finishSave = resolve
      })
    )
    try {
      wrapper.getComponent(ModelsSection).vm.$emit('edit', 1)
      await flushPromises()
      await wrapper.get('#model-metadata-name').setValue('saved-model')
      await wrapper.get('#model-metadata-form').trigger('submit')
      await flushPromises()
      expect(mocks.saveModel).toHaveBeenCalledWith(
        expect.objectContaining({ id: 1, model_name: 'saved-model' })
      )

      await router.push('/models/vendors')
      await flushPromises()
      expect(router.currentRoute.value.path).toBe('/models/metadata')
      expect(wrapper.findComponent(ModelMetadataDrawer).exists()).toBe(true)
      expect(confirm).not.toHaveBeenCalled()

      finishSave(rows[0]!)
      await flushPromises()
      expect(wrapper.findComponent(ModelMetadataDrawer).exists()).toBe(false)
      await router.push('/models/vendors')
      expect(router.currentRoute.value.path).toBe('/models/vendors')
      expect(confirm).not.toHaveBeenCalled()
    } finally {
      finishSave(rows[0]!)
      wrapper.unmount()
      confirm.mockRestore()
    }
  })

  it('cancels a pending model search when switching to the vendor section', async () => {
    const { wrapper, router } = await setup()
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    try {
      await wrapper
        .get('input[name="admin-model-filter"]')
        .setValue('pending-model')
      await router.push('/models/vendors?vFilter=kept#vendors')
      await flushPromises()
      await vi.advanceTimersByTimeAsync(500)
      await flushPromises()
      expect(router.currentRoute.value.fullPath).toBe(
        '/models/vendors?vFilter=kept#vendors'
      )
    } finally {
      wrapper.unmount()
      vi.useRealTimers()
    }
  })

  it('returns to the last valid model page after its final row is deleted', async () => {
    mocks.models
      .mockResolvedValueOnce({ items: [rows[0]], total: 21, vendor_counts: {} })
      .mockResolvedValueOnce({ items: [], total: 20, vendor_counts: {} })
      .mockResolvedValueOnce({ items: [rows[1]], total: 20, vendor_counts: {} })
    const { wrapper, router } = await setup('/models/metadata?page=2#models')
    await wrapper.get('input[aria-label="Select model-1"]').setValue(true)
    await wrapper
      .get('button[aria-label="Delete selected models"]')
      .trigger('click')
    wrapper.getComponent({ name: 'ConfirmDialog' }).vm.$emit('confirm')
    await flushPromises()
    expect(mocks.deleteModel).toHaveBeenCalledWith(1)
    expect(router.currentRoute.value.query.page).toBeUndefined()
    expect(router.currentRoute.value.hash).toBe('#models')
    expect(mocks.models).toHaveBeenLastCalledWith(
      expect.objectContaining({ p: 1 }),
      expect.any(AbortSignal)
    )
    expect(wrapper.find('input[aria-label="Select model-2"]').exists()).toBe(
      true
    )
    wrapper.unmount()
  })

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
  it('keeps the other selected rows after a single-row action', async () => {
    const { wrapper } = await setup()
    const checkbox = (id: number) =>
      wrapper.get(`input[aria-label="Select model-${id}"]`)
    const hideRow = async (index: number) => {
      wrapper.findAllComponents(ActionMenu)[index]!.vm.$emit('select', 'hide')
      await flushPromises()
    }
    // The page-header menu comes first; row menus follow in row order.
    await checkbox(2).setValue(true)
    await hideRow(1)
    expect(mocks.setModelStatus).toHaveBeenCalledWith(1, 0)
    expect(checkbox(2).element).toHaveProperty('checked', true)

    await checkbox(2).setValue(false)
    await checkbox(1).setValue(true)
    await hideRow(2)
    expect(mocks.setModelStatus).toHaveBeenLastCalledWith(2, 0)
    expect(checkbox(1).element).toHaveProperty('checked', true)
    expect(checkbox(2).element).toHaveProperty('checked', false)
    wrapper.unmount()
  })
  it('switches to the vendors section without dropping the query', async () => {
    const { wrapper, router } = await setup('/models/metadata?vFilter=open')
    await wrapper.get('#model-management-panel-tab-vendors').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/models/vendors')
    expect(router.currentRoute.value.query).toEqual({ vFilter: 'open' })
    expect(wrapper.findComponent(VendorsSection).exists()).toBe(true)
    wrapper.unmount()
  })

  it('shows the pricing tab only to root and keeps it on the same workspace', async () => {
    const { wrapper, router } = await setup()
    await wrapper.get('#model-management-panel-tab-pricing').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/models/pricing')
    expect(wrapper.findComponent(VendorsSection).exists()).toBe(false)
    wrapper.unmount()

    authState.isRoot = false
    const admin = await setup()
    expect(
      admin.wrapper.find('#model-management-panel-tab-pricing').exists()
    ).toBe(false)
    admin.wrapper.unmount()
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
