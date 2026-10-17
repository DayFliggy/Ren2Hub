import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ChannelFormModal from '../ChannelFormModal.vue'
import type { AdminChannel } from '@/types/console'

const permission = vi.hoisted(() => ({ sensitive: true }))
vi.mock('@/api/adminManagement', () => ({
  metadataApi: { groups: vi.fn().mockResolvedValue([]) },
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isAdmin: true,
    hasPermission: (resource: string, action: string) =>
      resource === 'channel' &&
      action === 'sensitive_write' &&
      permission.sensitive,
  }),
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    isFeatureEnabled: () => true,
    featureStatus: () => 'disabled',
  }),
}))

let wrapper: VueWrapper | undefined
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  permission.sensitive = true
})

async function form(
  type: number,
  setting: Record<string, unknown> = {},
  settings: Record<string, unknown> = {}
) {
  const save = vi.fn().mockResolvedValue(true)
  wrapper = mount(ChannelFormModal, {
    props: {
      open: true,
      editing: {
        id: 101,
        name: 'Provider fixture',
        supplier: 'Fixture',
        used_quota: '0',
        balance: 0,
        response_time: 0,
        test_time: 0,
        type,
        status: 1,
        base_url: '',
        models: 'fixture',
        model_mapping: '',
        priority: 0,
        weight: 1,
        capacity_total: 20,
        capacity_used: 0,
        channel_ratio: '1',
        setting: JSON.stringify(setting),
        settings: JSON.stringify(settings),
        other: 'global',
      } as AdminChannel,
      save,
      fetchModels: vi.fn(),
    },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale: 'en',
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
      stubs: { Teleport: true, GatewayChannelPanel: true },
    },
  })
  await flushPromises()
  return save
}
async function saveForm() {
  const button = wrapper!
    .findAll('button')
    .find((button) => button.text() === 'channels.saveChannel')
  expect(button).toBeDefined()
  await button!.trigger('click')
  await flushPromises()
}

describe('provider channel configuration round trip', () => {
  it('keeps InvokeModel for old AWS channels and saves an explicit Mantle choice', async () => {
    const save = await form(33, { proxy: 'http://proxy.example' })
    expect(wrapper!.text()).toContain(
      'APIKey|Region or AccessKey|SecretAccessKey|Region'
    )
    const select = wrapper!.get('#channel-aws-api-type')
    expect((select.element as HTMLSelectElement).value).toBe('invoke_model')
    await select.setValue('mantle')
    await saveForm()
    expect(JSON.parse(save.mock.calls[0]![0].setting)).toEqual({
      proxy: 'http://proxy.example',
      aws_api_type: 'mantle',
    })
    expect(save.mock.calls[0]![0].settings).toBeUndefined()
  })
  it('loads Mantle and can switch back without changing other channel settings', async () => {
    const save = await form(33, {
      aws_api_type: 'mantle',
      tls_insecure_skip_verify: true,
    })
    expect(
      (wrapper!.get('#channel-aws-api-type').element as HTMLSelectElement).value
    ).toBe('mantle')
    await wrapper!.get('#channel-aws-api-type').setValue('invoke_model')
    await saveForm()
    expect(JSON.parse(save.mock.calls[0]![0].setting)).toEqual({
      aws_api_type: 'invoke_model',
      tls_insecure_skip_verify: true,
    })
  })
  it('saves and clears an optional Vertex project, keeping region and credential contracts separate', async () => {
    const save = await form(
      41,
      { vertex_project_id: 'old-project', proxy: '' },
      { vertex_key_type: 'api_key' }
    )
    const project = wrapper!.get('input[name="channel-vertex-project-id"]')
    expect((project.element as HTMLInputElement).value).toBe('old-project')
    await project.setValue(' new-project ')
    await wrapper!.get('input[name="channel-vertex-region"]').setValue('eu')
    await saveForm()
    expect(JSON.parse(save.mock.calls[0]![0].setting)).toEqual({
      vertex_project_id: 'new-project',
      proxy: '',
    })
    expect(save.mock.calls[0]![0].other).toBe('eu')
    await project.setValue('')
    await saveForm()
    expect(JSON.parse(save.mock.calls[1]![0].setting).vertex_project_id).toBe(
      ''
    )
  })
  it('service-account mode clears only the API-key project and persists the key type', async () => {
    const save = await form(
      41,
      { vertex_project_id: 'api-key-project' },
      { vertex_key_type: 'api_key', allow_speed: true }
    )
    await wrapper!.get('#channel-vertex-key-type').setValue('json')
    expect(
      wrapper!.find('input[name="channel-vertex-project-id"]').exists()
    ).toBe(false)
    await saveForm()
    expect(JSON.parse(save.mock.calls[0]![0].setting)).toEqual({
      vertex_project_id: '',
    })
    expect(JSON.parse(save.mock.calls[0]![0].settings)).toEqual({
      vertex_key_type: 'json',
      allow_speed: true,
    })
  })
  it('prevents sensitive edits and preserves hidden settings during routing-only saves', async () => {
    permission.sensitive = false
    const save = await form(
      41,
      { vertex_project_id: 'protected-project' },
      { vertex_key_type: 'api_key' }
    )
    expect(
      wrapper!.get('#channel-vertex-key-type').attributes('disabled')
    ).toBeDefined()
    expect(
      wrapper!
        .get('input[name="channel-vertex-project-id"]')
        .attributes('disabled')
    ).toBeDefined()
    await saveForm()
    expect(save.mock.calls[0]![0].setting).toBeUndefined()
    expect(save.mock.calls[0]![0].settings).toBeUndefined()
    expect(save.mock.calls[0]![0].other).toBeUndefined()
  })
})
