import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import ChannelFormModal from '@/components/console/channels/ChannelFormModal.vue'
import type { AdminChannel } from '@/types/console'

const mocks = vi.hoisted(() => ({
  groups: vi.fn(),
  enabled: true,
  isAdmin: true,
  canSensitiveWrite: false,
}))
vi.mock('@/api/adminManagement', () => ({
  metadataApi: { groups: mocks.groups },
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isAdmin: mocks.isAdmin,
    hasPermission: (_resource: string, action: string) =>
      action === 'sensitive_write' && mocks.canSensitiveWrite,
  }),
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    isFeatureEnabled: () => mocks.enabled,
    featureStatus: () => (mocks.enabled ? 'live' : 'disabled'),
  }),
}))
function setup(patch: Partial<AdminChannel> = {}) {
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
    ...patch,
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
  mocks.canSensitiveWrite = false
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
  it('saves supported passthrough fields while retaining unrelated settings', async () => {
    mocks.canSensitiveWrite = true
    const { wrapper, save } = setup({
      type: 14,
      settings: JSON.stringify({ allow_speed: true, retained: 'value' }),
    })
    expect(
      wrapper
        .find(
          '[role="switch"][aria-label="channels.fieldSettingsLabels.disable_store"]'
        )
        .exists()
    ).toBe(false)
    await wrapper
      .get(
        '[role="switch"][aria-label="channels.fieldSettingsLabels.allow_speed"]'
      )
      .trigger('click')
    await wrapper
      .get(
        '[role="switch"][aria-label="channels.fieldSettingsLabels.allow_inference_geo"]'
      )
      .trigger('click')
    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('channels.saveChannel'))!
      .trigger('click')
    await flushPromises()
    expect(JSON.parse(save.mock.calls[0]![0].settings)).toEqual({
      allow_speed: false,
      allow_inference_geo: true,
      retained: 'value',
    })
    wrapper.unmount()
  })
  it('saves the Ollama chat switch only for an Ollama channel', async () => {
    mocks.canSensitiveWrite = true
    const { wrapper, save } = setup({
      type: 4,
      settings: JSON.stringify({ retained: 1 }),
    })
    await wrapper
      .get(
        '[role="switch"][aria-label="channels.fieldSettingsLabels.ollama_openai_chat"]'
      )
      .trigger('click')
    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('channels.saveChannel'))!
      .trigger('click')
    await flushPromises()
    expect(JSON.parse(save.mock.calls[0]![0].settings)).toEqual({
      retained: 1,
      ollama_openai_chat: true,
    })
    wrapper.unmount()
  })
  it('appends keys and updates strategy without exposing existing keys', async () => {
    mocks.canSensitiveWrite = true
    const { wrapper, save } = setup({
      channel_info: {
        is_multi_key: true,
        multi_key_mode: 'random',
        multi_key_size: 2,
      },
    })
    expect(
      (
        wrapper.get('textarea[name="admin-channel-key-multi"]')
          .element as HTMLTextAreaElement
      ).value
    ).toBe('')
    await wrapper
      .get('textarea[name="admin-channel-key-multi"]')
      .setValue('new-fixture-key')
    const strategy = wrapper
      .findAll('select')
      .find((select) => select.find('option[value="polling"]').exists())!
    await strategy.setValue('polling')
    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('channels.saveChannel'))!
      .trigger('click')
    await flushPromises()
    expect(save.mock.calls[0]![0]).toMatchObject({
      key: 'new-fixture-key',
      key_mode: 'append',
      multi_key_mode: 'polling',
    })
    wrapper.unmount()
  })
  it('preserves existing settings and plugin bindings when enabling Responses WebSocket', async () => {
    mocks.canSensitiveWrite = true
    const { wrapper, save } = setup({
      type: 60,
      setting: JSON.stringify({
        proxy: 'http://proxy.test',
        task_extend_plugin_keys: ['keep'],
        responses_websocket_enabled: false,
      }),
    })
    await wrapper
      .get('[role="switch"][aria-label="channels.responsesWebSocket"]')
      .trigger('click')
    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('channels.saveChannel'))!
      .trigger('click')
    await flushPromises()
    expect(JSON.parse(save.mock.calls[0]![0].setting)).toEqual({
      proxy: 'http://proxy.test',
      task_extend_plugin_keys: ['keep'],
      responses_websocket_enabled: true,
    })
    wrapper.unmount()
  })
  it('omits sensitive settings on an ordinary routing edit', async () => {
    const { wrapper, save } = setup({
      type: 60,
      setting: JSON.stringify({
        proxy: 'http://proxy.test',
        task_extend_plugin_keys: ['keep'],
        responses_websocket_enabled: true,
      }),
    })
    expect(
      wrapper
        .get('[role="switch"][aria-label="channels.responsesWebSocket"]')
        .attributes('disabled')
    ).toBeDefined()
    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('channels.saveChannel'))!
      .trigger('click')
    await flushPromises()
    expect(save.mock.calls[0]![0]).not.toHaveProperty('setting')
    wrapper.unmount()
  })
  it('saves advanced routes without losing other channel settings', async () => {
    mocks.canSensitiveWrite = true
    const { wrapper, save } = setup({
      type: 58,
      settings: JSON.stringify({
        allow_speed: true,
        advanced_custom: {
          advanced_routes: [
            {
              incoming_path: '/v1/responses',
              upstream_path: '/v1/responses',
              converter: 'none',
            },
          ],
        },
      }),
    })
    const editor = wrapper.findComponent({ name: 'AdvancedChannelEditor' })
    await editor.findAll('input')[1]!.setValue('/provider/responses')
    await wrapper
      .findAll('button')
      .find((button) => button.text().includes('channels.saveChannel'))!
      .trigger('click')
    await flushPromises()
    expect(JSON.parse(save.mock.calls[0]![0].settings)).toEqual({
      allow_speed: true,
      advanced_custom: {
        advanced_routes: [
          {
            incoming_path: '/v1/responses',
            upstream_path: '/provider/responses',
            converter: 'none',
          },
        ],
      },
    })
    wrapper.unmount()
  })
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
