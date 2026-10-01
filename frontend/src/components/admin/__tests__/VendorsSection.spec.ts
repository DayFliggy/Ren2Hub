import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import VendorDrawer from '../VendorDrawer.vue'
import VendorsSection from '../VendorsSection.vue'

const mocks = vi.hoisted(() => ({
  vendors: vi.fn(),
  vendorCounts: vi.fn(),
  deleteVendor: vi.fn(),
  saveVendor: vi.fn(),
  models: vi.fn(),
}))
vi.mock('@/api/adminManagement', () => ({ metadataApi: mocks }))

const ModalStub = {
  props: ['open', 'title'],
  emits: ['close'],
  template: '<div v-if="open"><slot /><slot name="footer" /></div>',
}
const vendor = (id: number, name: string, description = '') => ({
  id,
  name,
  description,
  icon: '',
  status: 1,
  created_time: null,
  updated_time: null,
})

async function setup(path = '/models/vendors') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
  })
  await router.push(path)
  const wrapper = mount(VendorsSection, {
    global: {
      plugins: [
        router,
        createI18n({ legacy: false, locale: 'en', missingWarn: false }),
      ],
      stubs: { ConsoleModal: ModalStub },
    },
  })
  await flushPromises()
  return { wrapper, router }
}
async function chooseRowAction(
  wrapper: VueWrapper,
  index: number,
  label: string
) {
  await wrapper
    .findAll('button[aria-label="Open menu"]')
    [index]!.trigger('click')
  await flushPromises()
  const item = [...document.body.querySelectorAll('[role="menuitem"]')].find(
    (node) => node.textContent?.trim() === label
  ) as HTMLElement
  item.click()
  await flushPromises()
}

beforeEach(() => {
  vi.clearAllMocks()
  mocks.vendors.mockResolvedValue({
    items: [vendor(1, 'Acme', 'Model maker'), vendor(2, 'Beta')],
    total: 2,
  })
  mocks.vendorCounts.mockResolvedValue({ '1': 3 })
})

describe('VendorsSection URL state', () => {
  it('restores vFilter/vPage/vPageSize and debounces keyword writes', async () => {
    mocks.vendors.mockResolvedValue({ items: [vendor(1, 'Acme')], total: 120 })
    const { wrapper, router } = await setup(
      '/models/vendors?vFilter=ac&vPage=2&vPageSize=50&tab=x#list'
    )
    expect(mocks.vendors).toHaveBeenLastCalledWith(
      { p: 2, page_size: 50, keyword: 'ac' },
      expect.any(AbortSignal)
    )
    const search = wrapper.get('input[name="vendor-search"]')
    expect(search.element).toHaveProperty('value', 'ac')
    await search.setValue('beta ')
    await flushPromises()
    expect(router.currentRoute.value.query.vFilter).toBe('ac')
    await new Promise((resolve) => setTimeout(resolve, 350))
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({
      vFilter: 'beta',
      vPageSize: '50',
      tab: 'x',
    })
    expect(router.currentRoute.value.hash).toBe('#list')
    expect(mocks.vendors).toHaveBeenLastCalledWith(
      { p: 1, page_size: 50, keyword: 'beta' },
      expect.any(AbortSignal)
    )
    wrapper.unmount()
  })

  it('falls back to defaults for invalid pagination and links linked counts', async () => {
    const { wrapper } = await setup('/models/vendors?vPage=-1&vPageSize=7')
    expect(mocks.vendors).toHaveBeenCalledWith(
      { p: 1, page_size: 20, keyword: undefined },
      expect.any(AbortSignal)
    )
    const tableLinks = wrapper
      .findAll('tbody a')
      .map((link) => [link.attributes('href'), link.text()])
    expect(tableLinks).toEqual([
      ['/models/metadata?vendor=1', '3'],
      ['/models/metadata?vendor=2', '0'],
    ])
    wrapper.unmount()
  })

  it('surfaces a vendor count failure and recovers on retry', async () => {
    mocks.vendorCounts.mockRejectedValueOnce(new Error('counts unavailable'))
    const { wrapper } = await setup()
    expect(wrapper.get('[role="alert"]').text()).toContain('counts unavailable')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Retry')!
      .trigger('click')
    await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('Acme')
    wrapper.unmount()
  })
})

describe('VendorsSection mutations', () => {
  it('confirms deletion, keeps the dialog open on failure and reloads on success', async () => {
    mocks.deleteVendor
      .mockRejectedValueOnce(new Error('vendor in use'))
      .mockResolvedValueOnce(undefined)
    const { wrapper } = await setup()
    await chooseRowAction(wrapper, 0, 'Delete')
    const dialog = wrapper.getComponent(ConfirmDialog)
    expect(dialog.props('open')).toBe(true)
    expect(dialog.props('message')).toContain('"Acme"')
    expect(mocks.deleteVendor).not.toHaveBeenCalled()
    dialog.vm.$emit('confirm')
    await flushPromises()
    expect(mocks.deleteVendor).toHaveBeenCalledWith(1)
    expect(dialog.props('open')).toBe(true)
    expect(dialog.text()).toContain('vendor in use')
    expect(wrapper.emitted('changed')).toBeUndefined()
    dialog.vm.$emit('confirm')
    await flushPromises()
    expect(dialog.props('open')).toBe(false)
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(mocks.vendors).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('opens the editor from the name and forwards open-model after closing it', async () => {
    const { wrapper } = await setup()
    await wrapper.get('[data-vendor-name]').trigger('click')
    const drawer = wrapper.getComponent(VendorDrawer)
    expect(drawer.props()).toMatchObject({
      vendor: { id: 1, name: 'Acme' },
      linkedCount: 3,
    })
    drawer.vm.$emit('open-model', { id: 9 })
    await flushPromises()
    expect(wrapper.findComponent(VendorDrawer).exists()).toBe(false)
    expect(wrapper.emitted('open-model')).toEqual([[{ id: 9 }]])
    wrapper.unmount()
  })

  it('exposes openCreate and reloads after a save', async () => {
    const { wrapper } = await setup()
    ;(wrapper.vm as unknown as { openCreate: () => void }).openCreate()
    await flushPromises()
    const drawer = wrapper.getComponent(VendorDrawer)
    expect(drawer.props('vendor')).toBeNull()
    drawer.vm.$emit('saved')
    await flushPromises()
    expect(wrapper.findComponent(VendorDrawer).exists()).toBe(false)
    expect(wrapper.emitted('changed')).toHaveLength(1)
    expect(mocks.vendors).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })
})
