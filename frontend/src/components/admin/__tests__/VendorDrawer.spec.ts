import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import VendorDrawer from '../VendorDrawer.vue'

const mocks = vi.hoisted(() => ({ saveVendor: vi.fn(), models: vi.fn() }))
vi.mock('@/api/adminManagement', () => ({ metadataApi: mocks }))

const ModalStub = {
  props: ['open', 'title', 'subtitle'],
  emits: ['close'],
  template:
    '<div v-if="open" :data-modal="title"><slot /><slot name="footer" /></div>',
}
const vendor = {
  id: 7,
  name: 'Acme',
  description: 'Old description',
  icon: 'OpenAI',
  status: 0,
  created_time: 1700000000,
  updated_time: 1700000500,
}
const linkedModel = (id: number, name_rule: number, matched_count: number) => ({
  id,
  model_name: `model-${id}`,
  name_rule,
  matched_count,
})

async function setup(props: {
  vendor: typeof vendor | null
  linkedCount: number
}) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/:pathMatch(.*)*', component: { template: '<div />' } }],
  })
  await router.push('/models/vendors')
  const wrapper = mount(VendorDrawer, {
    props,
    global: {
      plugins: [
        router,
        createI18n({ legacy: false, locale: 'en', missingWarn: false }),
      ],
      stubs: { ConsoleModal: ModalStub },
    },
  })
  return { wrapper, router }
}
function button(wrapper: VueWrapper, text: string) {
  const found = wrapper
    .findAll('button')
    .find((item) => item.text().trim() === text)
  if (!found) throw new Error(`button "${text}" not found`)
  return found
}
const discardOpen = (wrapper: VueWrapper) =>
  wrapper.getComponent(ConfirmDialog).props('open')

beforeEach(() => {
  vi.clearAllMocks()
  mocks.saveVendor.mockResolvedValue({})
  mocks.models.mockResolvedValue({
    items: [linkedModel(1, 0, 0), linkedModel(2, 1, 4)],
    total: 2,
    vendor_counts: {},
  })
})

describe('VendorDrawer details', () => {
  it('saves only editable fields and keeps the stored status', async () => {
    const { wrapper } = await setup({ vendor, linkedCount: 3 })
    await wrapper.get('input[name="vendor-name"]').setValue('  Acme Labs  ')
    await wrapper
      .get('textarea[name="vendor-description"]')
      .setValue('New description')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.saveVendor).toHaveBeenCalledTimes(1)
    expect(mocks.saveVendor.mock.calls[0]![0]).toStrictEqual({
      id: 7,
      name: 'Acme Labs',
      description: 'New description',
      icon: 'OpenAI',
      status: 0,
    })
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })

  it('creates without tabs, rejects a blank name and defaults status to 1', async () => {
    const { wrapper } = await setup({ vendor: null, linkedCount: 0 })
    expect(wrapper.find('[role="tablist"]').exists()).toBe(false)
    expect(wrapper.find('[data-modal="Create Vendor"]').exists()).toBe(true)
    await wrapper.get('input[name="vendor-name"]').setValue('   ')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.saveVendor).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBe('Vendor name is required')
    await wrapper.get('input[name="vendor-name"]').setValue('Acme')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(mocks.saveVendor.mock.calls[0]![0]).toStrictEqual({
      name: 'Acme',
      description: '',
      icon: '',
      status: 1,
    })
    wrapper.unmount()
  })

  it('shows the server error and stays open when saving fails', async () => {
    mocks.saveVendor.mockRejectedValue(new Error('vendor name exists'))
    const { wrapper } = await setup({ vendor, linkedCount: 0 })
    await wrapper.get('input[name="vendor-name"]').setValue('Other')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(wrapper.get('[role="alert"]').text()).toBe('vendor name exists')
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(wrapper.get('input[name="vendor-name"]').element).toHaveProperty(
      'value',
      'Other'
    )
    wrapper.unmount()
  })
})

describe('VendorDrawer leave guard', () => {
  it('keeps edits when route navigation is canceled and releases its guard on unmount', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const { wrapper, router } = await setup({ vendor, linkedCount: 0 })
    await wrapper.get('input[name="vendor-name"]').setValue('Unsaved vendor')
    await router.push('/dashboard')
    expect(router.currentRoute.value.path).toBe('/models/vendors')
    expect(confirm).toHaveBeenCalledTimes(1)
    const unload = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(unload)
    expect(unload.defaultPrevented).toBe(true)
    wrapper.unmount()
    await router.push('/dashboard')
    expect(router.currentRoute.value.path).toBe('/dashboard')
    const afterUnmount = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(afterUnmount)
    expect(afterUnmount.defaultPrevented).toBe(false)
    confirm.mockRestore()
  })

  it('closes immediately when nothing changed', async () => {
    const { wrapper } = await setup({ vendor, linkedCount: 0 })
    await button(wrapper, 'Close').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(discardOpen(wrapper)).toBe(false)
    wrapper.unmount()
  })

  it('asks before discarding unsaved edits on close', async () => {
    const { wrapper } = await setup({ vendor, linkedCount: 0 })
    await wrapper.get('input[name="vendor-icon"]').setValue('Anthropic')
    await button(wrapper, 'Close').trigger('click')
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(discardOpen(wrapper)).toBe(true)
    wrapper.getComponent(ConfirmDialog).vm.$emit('cancel')
    await flushPromises()
    expect(discardOpen(wrapper)).toBe(false)
    expect(wrapper.emitted('close')).toBeUndefined()
    await button(wrapper, 'Close').trigger('click')
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    wrapper.unmount()
  })

  it('guards the filtered model list link and keeps the vendor deep link', async () => {
    const { wrapper, router } = await setup({ vendor, linkedCount: 3 })
    await wrapper.get('input[name="vendor-name"]').setValue('Edited')
    await button(wrapper, 'Linked models (3)').trigger('click')
    await flushPromises()
    await button(wrapper, 'Open filtered model list').trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.path).toBe('/models/vendors')
    expect(discardOpen(wrapper)).toBe(true)
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(router.currentRoute.value.path).toBe('/models/metadata')
    expect(router.currentRoute.value.query).toEqual({ vendor: '7' })
    wrapper.unmount()
  })
})

describe('VendorDrawer linked models', () => {
  it('loads the vendor page lazily and opens a model without closing', async () => {
    const { wrapper } = await setup({ vendor, linkedCount: 3 })
    expect(mocks.models).not.toHaveBeenCalled()
    await button(wrapper, 'Linked models (3)').trigger('click')
    await flushPromises()
    expect(mocks.models).toHaveBeenCalledWith(
      { vendor: 7, p: 1, page_size: 10 },
      expect.any(AbortSignal)
    )
    expect(wrapper.text()).toContain('3 linked model records')
    expect(
      wrapper.findAll('button').some((b) => b.text() === 'Save metadata')
    ).toBe(false)
    const cells = wrapper
      .findAll('tbody tr')
      .map((row) => row.findAll('td').map((cell) => cell.text()))
    expect(cells).toEqual([
      ['model-1', 'Exact', '—'],
      ['model-2', 'Prefix', '4'],
    ])
    await button(wrapper, 'model-2').trigger('click')
    expect(wrapper.emitted('open-model')).toEqual([[{ id: 2 }]])
    expect(wrapper.emitted('close')).toBeUndefined()
    wrapper.unmount()
  })

  it('asks before opening a model while details are dirty', async () => {
    const { wrapper } = await setup({ vendor, linkedCount: 3 })
    await wrapper.get('input[name="vendor-name"]').setValue('Edited')
    await button(wrapper, 'Linked models (3)').trigger('click')
    await flushPromises()
    await button(wrapper, 'model-1').trigger('click')
    expect(wrapper.emitted('open-model')).toBeUndefined()
    wrapper.getComponent(ConfirmDialog).vm.$emit('confirm')
    await flushPromises()
    expect(wrapper.emitted('open-model')).toEqual([[{ id: 1 }]])
    wrapper.unmount()
  })
})
