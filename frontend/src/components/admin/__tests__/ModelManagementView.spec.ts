import { flushPromises, mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ModelManagementView from '@/views/admin/ModelManagementView.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import AutomaticPricingPanel from '@/components/admin/AutomaticPricingPanel.vue'

const mocks = vi.hoisted(() => ({
  auth: { isRoot: true },
  models: vi.fn(),
  allVendors: vi.fn(),
  setModelStatus: vi.fn(),
  deleteModel: vi.fn(),
}))
vi.mock('@/api/adminManagement', () => ({ metadataApi: mocks }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.auth }))
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
    routes: [{ path: '/models/metadata', component: ModelManagementView }],
  })
  await router.push(path)
  await router.isReady()
  const wrapper = mount(ModelManagementView, {
    global: {
      plugins: [
        router,
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
        AutomaticPricingPanel: true,
        ModelMetadataDrawer: true,
        MetadataSyncDialog: true,
        MissingModelsDialog: true,
        ManagementDialog: true,
        ConfirmDialog: true,
        FieldVisibilityMenu: true,
      },
    },
  })
  await flushPromises()
  return { wrapper, router }
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.auth.isRoot = true
  mocks.models.mockResolvedValue({
    items: rows,
    total: 2,
    vendor_counts: { '1': 2 },
  })
  mocks.allVendors.mockResolvedValue([
    { id: 1, name: 'Test vendor', description: '', status: 1, icon: '' },
  ])
  mocks.setModelStatus.mockImplementation(async (id: number) => {
    if (id === 2) throw new Error('permission denied')
  })
})
describe('metadata list routes and bulk selection', () => {
  it('reloads model prices after the automatic pricing task finishes', async () => {
    const { wrapper } = await setup()
    mocks.models.mockClear()
    wrapper.getComponent(AutomaticPricingPanel).vm.$emit('updated')
    await flushPromises()
    expect(mocks.models).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
  it('shows automatic pricing only for Root', async () => {
    const root = await setup()
    expect(root.wrapper.find('automatic-pricing-panel-stub').exists()).toBe(
      true
    )
    root.wrapper.unmount()
    mocks.auth.isRoot = false
    const admin = await setup()
    expect(admin.wrapper.find('automatic-pricing-panel-stub').exists()).toBe(
      false
    )
    admin.wrapper.unmount()
  })
  it('restores filters from the URL and preserves management deep links and hash', async () => {
    const { wrapper, router } = await setup(
      '/models/metadata?keyword=model&status=1&manage=vendors#models'
    )
    expect(mocks.models).toHaveBeenCalledWith(
      expect.objectContaining({ keyword: 'model', status: '1' }),
      expect.any(AbortSignal)
    )
    expect(wrapper.find('management-dialog-stub').attributes('kind')).toBe(
      'vendors'
    )
    expect(
      wrapper.get('input[placeholder="Search model names"]').element
    ).toHaveProperty('value', 'model')
    const filter = wrapper
      .findAllComponents(FilterSelect)
      .find((select) => select.props('label') === 'Status')!
    filter.vm.$emit('update:modelValue', '0')
    await flushPromises()
    expect(router.currentRoute.value.query).toMatchObject({
      keyword: 'model',
      status: '0',
      p: '1',
      manage: 'vendors',
    })
    expect(router.currentRoute.value.hash).toBe('#models')
    wrapper.unmount()
  })
  it('retains only failed model selections after a partially failed batch', async () => {
    const { wrapper } = await setup()
    await wrapper.get('input[aria-label="Select model-1"]').setValue(true)
    await wrapper.get('input[aria-label="Select model-2"]').setValue(true)
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Disable')!
      .trigger('click')
    await flushPromises()
    expect(mocks.setModelStatus).toHaveBeenCalledTimes(2)
    expect(
      wrapper.get('input[aria-label="Select model-1"]').element
    ).toHaveProperty('checked', false)
    expect(
      wrapper.get('input[aria-label="Select model-2"]').element
    ).toHaveProperty('checked', true)
    expect(wrapper.text()).toContain('1 succeeded, 1 failed')
    expect(wrapper.text()).toContain('model-2: permission denied')
    wrapper.unmount()
  })
})
