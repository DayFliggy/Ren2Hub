import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ChannelFormModal from '@/components/console/channels/ChannelFormModal.vue'
import type { AdminChannel } from '@/types/console'

vi.mock('@/api/adminManagement', () => ({
  metadataApi: { groups: vi.fn().mockResolvedValue([]) },
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isAdmin: true,
    hasPermission: (resource: string) => resource === 'channel',
  }),
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    isFeatureEnabled: () => true,
    featureStatus: () => 'live',
  }),
}))

function channel(modelMapping: string): AdminChannel {
  return {
    id: 9,
    name: 'openai-mapping',
    type: 1,
    channel_ratio: '1',
    max_concurrency: 0,
    in_flight: 0,
    supplier: 'OpenAI',
    lab: { id: 'openai', name: 'OpenAI', source: 'default' },
    status: 1,
    priority: 0,
    weight: 0,
    used_quota: 0,
    balance: 0,
    response_time: 0,
    test_time: 0,
    base_url: '',
    models: 'gpt-4o',
    model_mapping: modelMapping,
  }
}

async function open(modelMapping: string) {
  const save = vi.fn().mockResolvedValue(true)
  mount(ChannelFormModal, {
    attachTo: document.body,
    props: {
      open: true,
      editing: channel(modelMapping),
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
    },
  })
  await flushPromises()
  return save
}

function button(label: string) {
  return [
    ...document.body.querySelectorAll<HTMLButtonElement>(
      '[role="dialog"] button'
    ),
  ].find((item) => item.textContent?.trim() === label)!
}

function mappingText() {
  return document.body.querySelector<HTMLTextAreaElement>(
    'textarea[name="admin-channel-model-mapping"]'
  )
}

function mappingAlert() {
  return document.body.querySelector('#admin-channel-model-mapping-error')
    ?.textContent
}

async function typeMapping(text: string) {
  const area = mappingText()!
  area.value = text
  area.dispatchEvent(new Event('input'))
  area.dispatchEvent(new Event('blur'))
  await flushPromises()
}

async function rename(name: string) {
  const input = document.body.querySelector<HTMLInputElement>(
    '[role="dialog"] input[name="admin-channel-name"]'
  )!
  input.value = name
  input.dispatchEvent(new Event('input'))
  await flushPromises()
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('channel model mapping', () => {
  it('keeps a stored non-string mapping verbatim and blocks saving it', async () => {
    const stored = '{"gpt-4o":1}'
    const save = await open(stored)

    expect(mappingText()?.value).toBe(stored)
    expect(mappingAlert()).toContain('channels.modelMappingValueNotString')
    await rename('renamed')
    expect(button('channels.saveChannel').disabled).toBe(true)
    button('channels.saveChannel').click()
    button('channels.modelMappingVisual').click()
    button('channels.modelMappingJson').click()
    await flushPromises()

    expect(mappingText()?.value).toBe(stored)
    expect(save).not.toHaveBeenCalled()
  })

  it('submits the corrected mapping text once it is valid', async () => {
    const save = await open('{"gpt-4o":1}')
    const fixed = '{"gpt-4o":"gpt-4o-2024-08-06"}'

    await typeMapping(fixed)
    expect(mappingAlert()).toBeUndefined()
    button('channels.saveChannel').click()
    await flushPromises()

    expect(save).toHaveBeenCalledOnce()
    expect(save.mock.calls[0]![0].model_mapping).toBe(fixed)
  })

  it.each([
    ['{bad', 'channels.modelMappingInvalidJson'],
    ['["gpt-4o"]', 'channels.modelMappingNotObject'],
  ])(
    'reports %s typed in the JSON tab instead of keeping the old mapping',
    async (text, error) => {
      const save = await open('{"a":"b"}')
      button('channels.modelMappingJson').click()
      await flushPromises()

      await typeMapping(text)
      button('channels.modelMappingVisual').click()
      await flushPromises()

      expect(mappingText()?.value).toBe(text)
      expect(mappingAlert()).toContain(error)
      expect(button('channels.saveChannel').disabled).toBe(true)
      button('channels.saveChannel').click()
      await flushPromises()
      expect(save).not.toHaveBeenCalled()
    }
  )
})
