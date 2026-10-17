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
      (action === 'write' ||
        (action === 'sensitive_write' && permission.sensitive)),
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
        group: 'default',
        used_quota: 0,
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

describe('native provider configuration round trip', () => {
  it('stores Mantle and its credential type in settings, retaining transport setting', async () => {
    const save = await form(33, { proxy: 'http://proxy.example' })
    await wrapper!.get('#channel-aws-api-type').setValue('mantle')
    await wrapper!.get('#channel-aws-key-type').setValue('api_key')
    await saveForm()
    expect(JSON.parse(save.mock.calls[0]![0].settings)).toEqual({
      aws_api_type: 'mantle',
      aws_key_type: 'api_key',
    })
    expect(save.mock.calls[0]![0].setting).toBeUndefined()
    expect(save.mock.calls[0]![0]).not.toHaveProperty('channel_ratio')
    expect(save.mock.calls[0]![0]).not.toHaveProperty('capacity_total')
  })
  it('loads native Mantle and retains other provider options when switching back', async () => {
    const save = await form(
      33,
      { tls_insecure_skip_verify: true },
      { aws_api_type: 'mantle', aws_key_type: 'api_key' }
    )
    expect(
      (wrapper!.get('#channel-aws-api-type').element as HTMLSelectElement).value
    ).toBe('mantle')
    await wrapper!.get('#channel-aws-api-type').setValue('invoke_model')
    await saveForm()
    expect(JSON.parse(save.mock.calls[0]![0].settings)).toEqual({
      aws_api_type: 'invoke_model',
      aws_key_type: 'api_key',
    })
    expect(save.mock.calls[0]![0].setting).toBeUndefined()
  })
  it('saves and clears Vertex project ID in settings and region in other', async () => {
    const save = await form(
      41,
      { proxy: '' },
      { vertex_project_id: 'old-project', vertex_key_type: 'api_key' }
    )
    const project = wrapper!.get('input[name="channel-vertex-project-id"]')
    expect((project.element as HTMLInputElement).value).toBe('old-project')
    await project.setValue(' new-project ')
    await wrapper!.get('input[name="channel-vertex-region"]').setValue('eu')
    await saveForm()
    expect(JSON.parse(save.mock.calls[0]![0].settings)).toEqual({
      vertex_project_id: 'new-project',
      vertex_key_type: 'api_key',
    })
    expect(save.mock.calls[0]![0].other).toBe('eu')
    await project.setValue('')
    await saveForm()
    expect(JSON.parse(save.mock.calls[1]![0].settings).vertex_project_id).toBe(
      ''
    )
    expect(save.mock.calls[0]![0].setting).toBeUndefined()
  })
  it('service-account mode clears the API-key project while preserving unrelated flags', async () => {
    const save = await form(
      41,
      {},
      {
        vertex_project_id: 'api-key-project',
        vertex_key_type: 'api_key',
        allow_speed: true,
      }
    )
    await wrapper!.get('#channel-vertex-key-type').setValue('json')
    expect(
      wrapper!.find('input[name="channel-vertex-project-id"]').exists()
    ).toBe(false)
    await saveForm()
    expect(JSON.parse(save.mock.calls[0]![0].settings)).toEqual({
      vertex_project_id: '',
      vertex_key_type: 'json',
      allow_speed: true,
    })
  })
  it('prevents credential edits during group-only changes', async () => {
    permission.sensitive = false
    const save = await form(
      41,
      {},
      { vertex_project_id: 'protected-project', vertex_key_type: 'api_key' }
    )
    expect(
      wrapper!.get('#channel-vertex-key-type').attributes('disabled')
    ).toBeDefined()
    await wrapper!
      .get('input[name="admin-channel-group"]')
      .setValue(' default, vip,default ')
    await saveForm()
    expect(save.mock.calls[0]![0].group).toBe('default,vip')
    expect(save.mock.calls[0]![0].setting).toBeUndefined()
    expect(save.mock.calls[0]![0].settings).toBeUndefined()
    expect(save.mock.calls[0]![0].other).toBeUndefined()
  })
})
