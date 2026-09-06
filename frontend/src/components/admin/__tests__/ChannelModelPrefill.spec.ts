import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ChannelFormModal from '@/components/console/channels/ChannelFormModal.vue'
import type { AdminChannel } from '@/types/console'

const mocks = vi.hoisted(() => ({
  groups: vi.fn(),
  enabled: true,
  isAdmin: true,
}))
vi.mock('@/api/adminManagement', () => ({
  metadataApi: { groups: mocks.groups },
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ isAdmin: mocks.isAdmin }),
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ isFeatureEnabled: () => mocks.enabled }),
}))
function setup() {
  const save = vi.fn().mockResolvedValue(true)
  const editing = {
    id: 1,
    name: 'test',
    type: 1,
    status: 1,
    models: 'b,a',
    priority: 0,
    weight: 0,
    capacity_total: 20,
    capacity_used: 0,
    channel_ratio: 1,
  } as AdminChannel
  const wrapper = mount(ChannelFormModal, {
    props: { open: true, editing, save, fetchModels: vi.fn() },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
      stubs: {
        ConsoleModal: { template: '<div><slot /><slot name="footer" /></div>' },
      },
    },
  })
  return { wrapper, save, editing }
}
beforeEach(() => {
  vi.clearAllMocks()
  mocks.enabled = true
  mocks.isAdmin = true
  mocks.groups.mockResolvedValue([
    {
      id: 2,
      name: 'test group',
      type: 'model',
      items: ['a', 'c', 'c', 'b'],
      description: '',
    },
  ])
})
describe('channel model prefill integration', () => {
  it('appends once to the draft and only persists on channel save', async () => {
    const { wrapper, save, editing } = setup()
    await flushPromises()
    expect(mocks.groups).toHaveBeenCalledWith(expect.any(AbortSignal), 'model')
    await wrapper.get('#channel-prefill-group').setValue('2')
    const append = wrapper
      .findAll('button')
      .find((button) => button.text().includes('channels.appendPrefillGroup'))!
    await append.trigger('click')
    await append.trigger('click')
    expect(save).not.toHaveBeenCalled()
    expect(editing.models).toBe('b,a')
    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('channels.saveChannel'))!
      .trigger('click')
    await flushPromises()
    expect(save.mock.calls[0]![0].models).toBe('b,a,c')
    wrapper.unmount()
  })
  it('does not request prefill groups with a missing capability or non-admin identity', () => {
    mocks.enabled = false
    const first = setup().wrapper
    expect(mocks.groups).not.toHaveBeenCalled()
    expect(first.find('#channel-prefill-group').exists()).toBe(false)
    first.unmount()
    mocks.enabled = true
    mocks.isAdmin = false
    const second = setup().wrapper
    expect(mocks.groups).not.toHaveBeenCalled()
    second.unmount()
  })
  it('cancels the prefill read when the form closes', async () => {
    mocks.groups.mockImplementation(() => new Promise(() => {}))
    const { wrapper } = setup()
    const signal = mocks.groups.mock.calls[0]![0] as AbortSignal
    await wrapper.setProps({ open: false })
    expect(signal.aborted).toBe(true)
    wrapper.unmount()
  })
})
