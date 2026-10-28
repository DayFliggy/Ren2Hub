import { flushPromises, mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { afterEach, describe, expect, it, vi } from 'vitest'

import ChannelFormModal from '@/components/console/channels/ChannelFormModal.vue'
import enConsole from '@/i18n/locales/en/console'
import type { AdminChannel } from '@/types/console'

vi.mock('@/api/adminManagement', () => ({
  metadataApi: { groups: vi.fn().mockResolvedValue([]) },
}))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    isAdmin: true,
    hasPermission: () => true,
  }),
}))
vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    isFeatureEnabled: () => true,
    featureStatus: () => 'live',
  }),
}))

const channel: AdminChannel = {
  id: 91,
  name: 'advanced-channel',
  type: 58,
  channel_ratio: '1',
  max_concurrency: 0,
  in_flight: 0,
  lab: { id: 'advanced-custom', name: 'Advanced Custom', source: 'default' },
  status: 1,
  priority: 0,
  weight: 0,
  used_quota: 0,
  balance: 0,
  response_time: 0,
  test_time: 0,
  base_url: 'https://upstream.example',
  models: 'gpt-4o',
  model_mapping: '',
  setting: JSON.stringify({ pass_through_body_enabled: false }),
  settings: JSON.stringify({
    advanced_custom: {
      extension: 'preserved',
      advanced_routes: [
        {
          incoming_path: '/v1/responses',
          upstream_path: '/v1/responses',
          converter: 'none',
          pass_through_body_enabled: true,
          auth: {
            type: 'header',
            name: 'Authorization',
            value: 'Bearer {api_key}',
          },
        },
      ],
    },
  }),
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('advanced channel passthrough settings', () => {
  it('warns about global scope and roundtrips the global and route switches', async () => {
    const save = vi.fn().mockResolvedValue(true)
    const wrapper = mount(ChannelFormModal, {
      attachTo: document.body,
      props: { open: true, editing: channel, save, fetchModels: vi.fn() },
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'en',
            messages: { en: enConsole },
            missingWarn: false,
            fallbackWarn: false,
          }),
        ],
      },
    })
    await flushPromises()

    const section = [...document.body.querySelectorAll('button')].find((item) =>
      item.textContent?.includes('Advanced settings')
    )!
    section.click()
    await flushPromises()

    expect(document.body.textContent).toContain('Applies to every route')
    expect(document.body.textContent).toContain(
      'routes that require format conversion will fail'
    )
    const globalSwitch = document.body.querySelector<HTMLButtonElement>(
      '[role="switch"][aria-label="Pass through request body"]'
    )!
    globalSwitch.click()
    await flushPromises()

    const routeModels = document.body.querySelectorAll<HTMLInputElement>(
      '[role="dialog"] article input.route-input'
    )
    routeModels[2]!.value = 'gpt-4o'
    routeModels[2]!.dispatchEvent(new Event('change'))
    await flushPromises()

    const saveButton = [...document.body.querySelectorAll('button')].find(
      (item) => item.textContent?.includes('Save channel')
    )!
    saveButton.click()
    await flushPromises()

    expect(save).toHaveBeenCalledOnce()
    const payload = save.mock.calls[0]![0]
    expect(JSON.parse(payload.setting).pass_through_body_enabled).toBe(true)
    const settings = JSON.parse(payload.settings)
    expect(settings.advanced_custom).toMatchObject({
      extension: 'preserved',
      advanced_routes: [{ pass_through_body_enabled: true, converter: 'none' }],
    })
    wrapper.unmount()
  })
})
