import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import {
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from 'vitest'

import i18n, { loadMessageDomain, setLocale } from '@/i18n'

import * as pluginApi from '../api'
import TaskPlugins from '../TaskPlugins.vue'
import type { TaskPluginListItem } from '../types'

vi.mock('../api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api')>()
  return {
    TaskPluginUsageError: actual.TaskPluginUsageError,
    listTaskPlugins: vi.fn(),
    getTaskPluginEnabledOption: vi.fn(),
    setTaskPluginEnabledOption: vi.fn(),
    setTaskPluginStatus: vi.fn(),
    deleteTaskPluginVersion: vi.fn(),
    listMarketplaceSources: vi.fn(),
  }
})

const plugin: TaskPluginListItem = {
  meta: {
    key: 'fixture',
    name: 'Fixture Driver',
    version: '1.0.0',
    apiVersion: 1,
    models: ['m1'],
  },
  source: 'override',
  enabled: true,
  active: true,
  source_hash: 'hash',
  remark: '',
  runtime_status: 'registered',
  channel_count: 1,
  in_flight_count: 0,
}
let wrapper: VueWrapper | undefined

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => vi.clearAllMocks())
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

async function mountPage(
  options: { masterEnabled?: boolean; optionError?: boolean } = {}
) {
  vi.mocked(pluginApi.listTaskPlugins).mockResolvedValue([plugin])
  if (options.optionError)
    vi.mocked(pluginApi.getTaskPluginEnabledOption).mockRejectedValue(
      new Error('option unavailable')
    )
  else
    vi.mocked(pluginApi.getTaskPluginEnabledOption).mockResolvedValue(
      options.masterEnabled ?? true
    )
  vi.mocked(pluginApi.setTaskPluginEnabledOption).mockResolvedValue(null)
  wrapper = mount(TaskPlugins, {
    global: { plugins: [i18n], stubs: { Teleport: true, RouterLink: true } },
  })
  await flushPromises()
}

const master = () => wrapper!.get('[data-task-plugin-master] [role="switch"]')
const button = (text: string) =>
  wrapper!.findAll('button').find((item) => item.text() === text)

describe('task plugin page', () => {
  it('stays usable with the master switch off and enables it directly', async () => {
    await mountPage({ masterEnabled: false })
    expect(master().attributes('aria-checked')).toBe('false')
    expect(wrapper!.text()).toContain('Fixture Driver')
    expect(button('Upload plugin')).toBeDefined()
    await master().trigger('click')
    await flushPromises()
    expect(pluginApi.setTaskPluginEnabledOption).toHaveBeenCalledWith(true)
    expect(master().attributes('aria-checked')).toBe('true')
  })

  it('disables the master switch only after confirmation', async () => {
    await mountPage()
    await master().trigger('click')
    await flushPromises()
    expect(wrapper!.text()).toContain('Disable task plugins?')
    expect(pluginApi.setTaskPluginEnabledOption).not.toHaveBeenCalled()
    await button('Disable')!.trigger('click')
    await flushPromises()
    expect(pluginApi.setTaskPluginEnabledOption).toHaveBeenCalledWith(false)
  })

  it('keeps the plugin list when the master switch option cannot load', async () => {
    await mountPage({ optionError: true })
    expect(wrapper!.text()).toContain('Fixture Driver')
    expect(wrapper!.text()).toContain('option unavailable')
    expect(master().attributes('disabled')).toBeDefined()
  })

  it('cascades or forces a plugin disable only on an explicit choice', async () => {
    await mountPage()
    const usage = {
      channels: [{ id: 8, name: 'Shared gateway', type: 60 }],
      in_flight_count: 0,
    }
    vi.mocked(pluginApi.setTaskPluginStatus)
      .mockRejectedValueOnce(
        new pluginApi.TaskPluginUsageError('in use', usage)
      )
      .mockResolvedValue(null)
    await wrapper!.get('[aria-label="Enable plugin fixture"]').trigger('click')
    await flushPromises()
    await button('Disable')!.trigger('click')
    await flushPromises()
    expect(pluginApi.setTaskPluginStatus).toHaveBeenLastCalledWith(
      'fixture',
      false,
      undefined
    )
    expect(wrapper!.text()).toContain('Plugin is still in use')
    expect(wrapper!.text()).toContain('#8 Shared gateway')
    await button('Cascade disable channels')!.trigger('click')
    await flushPromises()
    expect(pluginApi.setTaskPluginStatus).toHaveBeenLastCalledWith(
      'fixture',
      false,
      { cascade: true }
    )
  })
})
