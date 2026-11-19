import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { afterEach, beforeAll, describe, expect, it, vi } from 'vitest'

import i18n, { loadMessageDomain, setLocale } from '@/i18n'
import * as pluginApi from '@/features/task-plugins/api'
import type { RequestPluginInstance } from '@/features/task-plugins/types'

import ChannelRequestPlugins from '../components/ChannelRequestPlugins.vue'

vi.mock('@/features/task-plugins/api', () => ({
  listRequestPluginInstances: vi.fn(),
}))

const instance = (
  id: string,
  extra: Partial<RequestPluginInstance> = {}
): RequestPluginInstance => ({
  id,
  name: `Instance ${id}`,
  plugin: 'guard',
  enabled: true,
  global: false,
  models: [],
  config: {},
  ...extra,
})

let wrapper: VueWrapper | undefined
beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
afterEach(() => wrapper?.unmount())

describe('channel request plugin bindings', () => {
  it('offers neither global nor plugin scope instances for binding', async () => {
    vi.mocked(pluginApi.listRequestPluginInstances).mockResolvedValue([
      instance('plain'),
      instance('global', { global: true }),
      instance('scoped', {
        plugin_scope: { models: ['claude'], channel_types: [1] },
      }),
    ])
    wrapper = mount(ChannelRequestPlugins, {
      props: { modelValue: [] },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    expect(wrapper.find('option[value="plain"]').exists()).toBe(true)
    expect(wrapper.find('option[value="global"]').exists()).toBe(false)
    expect(wrapper.find('option[value="scoped"]').exists()).toBe(false)
  })

  it('keeps a saved plugin scope binding removable and says where it runs', async () => {
    vi.mocked(pluginApi.listRequestPluginInstances).mockResolvedValue([
      instance('plain'),
      instance('scoped', {
        plugin_scope: { models: [], channel_types: [1] },
      }),
    ])
    let bound = ['scoped']
    wrapper = mount(ChannelRequestPlugins, {
      props: {
        modelValue: bound,
        'onUpdate:modelValue': (value: string[]) => {
          bound = value
          void wrapper!.setProps({ modelValue: value })
        },
      },
      global: { plugins: [i18n] },
    })
    await flushPromises()
    const notes = wrapper.findAll('[data-binding-follows-plugin]')
    expect(notes).toHaveLength(1)
    expect(notes[0]!.text()).toBe(
      'Runs where its plugin suggests; this binding has no effect'
    )
    // The old binding does not block adding another instance.
    await wrapper.get('select').setValue('plain')
    await wrapper
      .findAll('button')
      .find((button) => button.text() === 'Add instance')!
      .trigger('click')
    expect(bound).toEqual(['scoped', 'plain'])
    await wrapper
      .findAll('button')
      .filter((button) => button.text() === 'Remove')[0]!
      .trigger('click')
    expect(bound).toEqual(['plain'])
  })
})
