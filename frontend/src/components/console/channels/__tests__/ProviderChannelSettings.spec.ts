import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ChannelFormModal from '../ChannelFormModal.vue'
import type { AdminChannel } from '@/types/console'
import enConsole from '@/i18n/locales/en/console'
import zhConsole from '@/i18n/locales/zh-CN/console'

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
  settings: Record<string, unknown> = {},
  creating = false,
  locale = 'en'
) {
  const save = vi.fn().mockResolvedValue(true)
  wrapper = mount(ChannelFormModal, {
    props: {
      open: true,
      editing: creating
        ? null
        : ({
            id: 101,
            name: 'Provider fixture',
            channel_ratio: '1',
            max_concurrency: 0,
            in_flight: 0,
            lab: { id: 'openai', name: 'OpenAI', source: 'vendor' },
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
            other: '{"default":"global"}',
          } as AdminChannel),
      save,
      fetchModels: vi.fn(),
    },
    global: {
      plugins: [
        createI18n({
          legacy: false,
          locale,
          messages: {
            en: {
              channels: { modelsRequired: enConsole.channels.modelsRequired },
            },
            'zh-CN': {
              channels: { modelsRequired: zhConsole.channels.modelsRequired },
            },
          },
          missingWarn: false,
          fallbackWarn: false,
        }),
      ],
      stubs: { Teleport: true, GatewayChannelPanel: true },
    },
  })
  await flushPromises()
  if (creating) {
    if (type === 41) {
      await wrapper.get('button[aria-expanded]').trigger('click')
      await wrapper
        .findAll('[role="option"]')
        .find((option) => option.text().endsWith('Vertex AI'))!
        .trigger('click')
    }
    await wrapper
      .get('input[name="admin-channel-name"]')
      .setValue('New provider')
    await wrapper.get('input[name="admin-channel-key"]').setValue('fixture-key')
    const modelInput = wrapper.get(
      '.channel-form-section--models input[type="text"]'
    )
    await modelInput.setValue('fixture')
    await modelInput.trigger('keydown', { key: 'Enter' })
  }
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
  it('round trips the Vertex API version and preserves unknown settings', async () => {
    const save = await form(
      41,
      {},
      { vertex_api_version: 'v1beta1', provider_future: { enabled: true } }
    )
    const version = wrapper!.get('#channel-vertex-api-version')
    expect((version.element as HTMLSelectElement).value).toBe('v1beta1')
    await version.setValue('v1')
    await saveForm()
    expect(JSON.parse(save.mock.lastCall![0].settings)).toEqual({
      vertex_api_version: 'v1',
      provider_future: { enabled: true },
    })
    await version.setValue('')
    await saveForm()
    expect(JSON.parse(save.mock.lastCall![0].settings).vertex_api_version).toBe(
      ''
    )
  })
  it('validates native channel ratios exactly before saving', async () => {
    const save = await form(1)
    const ratio = wrapper!.get('input[name="channel-ratio"]')
    const submit = wrapper!
      .findAll('button')
      .find((button) => button.text() === 'channels.saveChannel')!
    for (const value of [
      '0',
      '-1',
      '1000.000000000000000001',
      '0.0000000000000000001',
      '1e3',
    ]) {
      await ratio.setValue(value)
      expect(submit.attributes('disabled'), value).toBeDefined()
      await submit.trigger('click')
      expect(save).not.toHaveBeenCalled()
    }
    for (const value of [
      '1000',
      '0.000000000000000001',
      '0.123456789012345678',
    ]) {
      await ratio.setValue(value)
      expect(submit.attributes('disabled'), value).toBeUndefined()
      await saveForm()
      expect(save.mock.lastCall![0].channel_ratio).toBe(value)
    }
  })
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
    expect(save.mock.calls[0]![0]).toHaveProperty('channel_ratio', '1')
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
  it('saves and clears Vertex project ID while preserving region JSON in other', async () => {
    const save = await form(
      41,
      { proxy: '' },
      { vertex_project_id: 'old-project', vertex_key_type: 'api_key' }
    )
    const project = wrapper!.get('input[name="channel-vertex-project-id"]')
    expect((project.element as HTMLInputElement).value).toBe('old-project')
    await project.setValue(' new-project ')
    const region = '{\n  "default": "eu",\n  "gemini-2.5-pro": "us-central1"\n}'
    await wrapper!
      .get('textarea[name="channel-vertex-region"]')
      .setValue(region)
    await saveForm()
    expect(JSON.parse(save.mock.calls[0]![0].settings)).toEqual({
      vertex_project_id: 'new-project',
      vertex_key_type: 'api_key',
    })
    expect(save.mock.calls[0]![0].other).toBe(region)
    await project.setValue('')
    await saveForm()
    expect(JSON.parse(save.mock.calls[1]![0].settings).vertex_project_id).toBe(
      ''
    )
    expect(save.mock.calls[0]![0].setting).toBeUndefined()
  })
  it.each([
    { creating: false, locale: 'en', error: 'Enter a Vertex region.' },
    { creating: true, locale: 'en', error: 'Enter a Vertex region.' },
    { creating: false, locale: 'zh-CN', error: '请填写 Vertex 区域。' },
    { creating: true, locale: 'zh-CN', error: '请填写 Vertex 区域。' },
  ])(
    'rejects blank Vertex regions (creating=$creating, locale=$locale)',
    async ({ creating, locale, error }) => {
      const save = await form(41, {}, {}, creating, locale)
      const region = wrapper!.get('textarea[name="channel-vertex-region"]')
      for (const blank of ['', ' \t\n ']) {
        await region.setValue(blank)
        expect(region.attributes('aria-invalid')).toBe('true')
        expect(wrapper!.get('#channel-vertex-region-error').text()).toBe(error)
        await saveForm()
        expect(save).not.toHaveBeenCalled()
      }
      const mapping = '{ "default": "global" }'
      await region.setValue(mapping)
      expect(wrapper!.find('#channel-vertex-region-error').exists()).toBe(false)
      await saveForm()
      expect(save).toHaveBeenCalledOnce()
      expect(save.mock.calls[0]![0].other).toBe(mapping)
    }
  )
  it('passes nonblank region text without converting it to JSON', async () => {
    const save = await form(41)
    await wrapper!.get('textarea[name="channel-vertex-region"]').setValue('eu')
    await saveForm()
    expect(save.mock.calls[0]![0].other).toBe('eu')
  })
  it.each([false, true])(
    'requires at least one model (creating=%s)',
    async (creating) => {
      const save = await form(1, {}, {}, creating)
      if (creating) {
        await wrapper!
          .get('button[aria-label="Remove fixture"]')
          .trigger('click')
      } else {
        await wrapper!
          .findAll('button')
          .find((button) => button.text() === 'channels.quickClearAll')!
          .trigger('click')
      }
      expect(
        wrapper!
          .findAll('[role="alert"]')
          .some((alert) => alert.text() === enConsole.channels.modelsRequired)
      ).toBe(true)
      await saveForm()
      expect(save).not.toHaveBeenCalled()
      const modelInput = wrapper!.get(
        '.channel-form-section--models input[type="text"]'
      )
      await modelInput.setValue(' , , ')
      await modelInput.trigger('keydown', { key: 'Enter' })
      await saveForm()
      expect(save).not.toHaveBeenCalled()
      await modelInput.setValue('gemini-2.5-pro')
      await modelInput.trigger('keydown', { key: 'Enter' })
      await saveForm()
      expect(save).toHaveBeenCalledOnce()
      expect(save.mock.calls[0]![0].models).toBe('gemini-2.5-pro')
    }
  )
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
  it('prevents credential edits during concurrency changes', async () => {
    permission.sensitive = false
    const save = await form(
      41,
      {},
      { vertex_project_id: 'protected-project', vertex_key_type: 'api_key' }
    )
    expect(
      wrapper!.get('#channel-vertex-key-type').attributes('disabled')
    ).toBeDefined()
    expect(
      wrapper!.get('#channel-vertex-api-version').attributes('disabled')
    ).toBeDefined()
    await wrapper!.get('input[name="channel-max-concurrency"]').setValue('4')
    await saveForm()
    expect(save.mock.calls[0]![0].max_concurrency).toBe(4)
    expect(save.mock.calls[0]![0]).not.toHaveProperty('group')
    expect(save.mock.calls[0]![0].setting).toBeUndefined()
    expect(save.mock.calls[0]![0].settings).toBeUndefined()
    expect(save.mock.calls[0]![0].other).toBeUndefined()
  })
})
