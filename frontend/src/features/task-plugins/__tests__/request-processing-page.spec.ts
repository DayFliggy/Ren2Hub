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

import { api } from '@/api/client'
import { useToast } from '@/composables/useToast'
import i18n, { loadMessageDomain, setLocale } from '@/i18n'

import * as pluginApi from '../api'
import RequestProcessing from '../RequestProcessing.vue'
import type { RequestPluginInstance, TaskPluginOption } from '../types'

vi.mock('../api', () => ({
  dryRunRequestPlugins: vi.fn(),
  getTaskPluginOptions: vi.fn(),
  listRequestPluginInstances: vi.fn(),
  saveRequestPluginInstances: vi.fn(),
}))

const plugins: TaskPluginOption[] = [
  {
    key: 'masker',
    name: 'Masker',
    transform: true,
    transform_hooks: ['request'],
    models: null,
  },
  { key: 'video', name: 'Video task', task: true, models: null },
]
const rows: RequestPluginInstance[] = [
  {
    id: 'a',
    name: 'First',
    plugin: 'masker',
    enabled: true,
    global: true,
    models: [],
    config: {},
  },
  {
    id: 'b',
    name: 'Second',
    plugin: 'masker',
    enabled: false,
    global: false,
    models: ['gpt-4.1-mini'],
    config: {},
  },
]
let wrapper: VueWrapper | undefined

beforeAll(async () => {
  await loadMessageDomain('console')
  await setLocale('en')
})
beforeEach(() => {
  vi.clearAllMocks()
  vi.mocked(pluginApi.listRequestPluginInstances).mockResolvedValue(
    structuredClone(rows)
  )
  vi.mocked(pluginApi.getTaskPluginOptions).mockResolvedValue(plugins)
  vi.mocked(pluginApi.saveRequestPluginInstances).mockImplementation(
    async (next) => next
  )
  vi.spyOn(api, 'get').mockResolvedValue(['gpt-4.1-mini'])
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
})

async function mountPage() {
  wrapper = mount(RequestProcessing, {
    global: { plugins: [i18n], stubs: { Teleport: true } },
  })
  await flushPromises()
  return wrapper
}

function button(page: VueWrapper, name: string) {
  const found = page
    .findAll('button')
    .find(
      (item) =>
        item.attributes('aria-label') === name || item.text().trim() === name
    )
  if (!found) throw new Error(`no button named ${name}`)
  return found
}

function savedLists() {
  return vi
    .mocked(pluginApi.saveRequestPluginInstances)
    .mock.calls.map(([list]) => list)
}

describe('request processing page', () => {
  it('saves the whole list at once when moving, toggling and deleting', async () => {
    const page = await mountPage()
    expect(page.findAll('[data-request-plugin-row]')).toHaveLength(2)
    expect(page.text()).toContain('Models: gpt-4.1-mini')

    await button(page, 'Move Second up').trigger('click')
    await flushPromises()
    expect(savedLists()[0]!.map((item) => item.id)).toEqual(['b', 'a'])

    await page
      .get('[role="switch"][aria-label="Enable First"]')
      .trigger('click')
    await flushPromises()
    const toggled = savedLists()[1]!
    expect(toggled.find((item) => item.id === 'a')?.enabled).toBe(false)

    await button(page, 'Delete First').trigger('click')
    await flushPromises()
    // Nothing is deleted before the confirmation.
    expect(savedLists()).toHaveLength(2)
    await button(page, 'Delete').trigger('click')
    await flushPromises()
    expect(savedLists()[2]!.map((item) => item.id)).toEqual(['b'])
    expect(page.findAll('[data-request-plugin-row]')).toHaveLength(1)
  })

  it('keeps the list and shows the gateway message when a save fails', async () => {
    vi.mocked(pluginApi.saveRequestPluginInstances).mockRejectedValue(
      new Error('instance name taken')
    )
    const page = await mountPage()
    await button(page, 'Move Second up').trigger('click')
    await flushPromises()
    const names = page
      .findAll('[data-request-plugin-row]')
      .map((row) => row.text())
    expect(names[0]).toContain('First')
    expect(useToast().toasts.map((item) => item.message)).toContain(
      'instance name taken'
    )
  })

  it('adds an instance for chosen channels and models without a group', async () => {
    const page = await mountPage()
    await button(page, 'Add').trigger('click')
    await flushPromises()
    // Only request processing plugins are offered.
    expect(page.find('[data-plugin-card="video"]').exists()).toBe(false)
    await page.get('[data-plugin-card="masker"]').trigger('click')
    await flushPromises()

    await page.get('input[type="radio"][value="channels"]').setValue()
    await page.get('input[type="radio"][value="chosen"]').setValue()
    await page.get('[data-instance-editor] form').trigger('submit')
    // A chosen-models scope with no model is not saved.
    expect(savedLists()).toHaveLength(0)
    expect(page.text()).toContain('Pick at least one model')

    const input = page.get('[data-instance-editor] input[aria-label]')
    await input.setValue('custom-model')
    await input.trigger('keydown', { key: 'Enter' })
    await page.get('[data-instance-editor] form').trigger('submit')
    await flushPromises()

    const added = savedLists()[0]!.at(-1)!
    expect(added).toEqual({
      id: '',
      name: 'Masker',
      plugin: 'masker',
      enabled: true,
      global: false,
      models: ['custom-model'],
      config: {},
    })
    expect(page.find('[data-instance-editor]').exists()).toBe(false)
  })

  it('keeps the editor open with the error when its save fails', async () => {
    vi.mocked(pluginApi.saveRequestPluginInstances).mockRejectedValue(
      new Error('plugin refused config')
    )
    const page = await mountPage()
    await button(page, 'Edit First').trigger('click')
    await flushPromises()
    await page.get('[data-instance-editor] form').trigger('submit')
    await flushPromises()
    expect(page.find('[data-instance-editor]').exists()).toBe(true)
    expect(page.get('[data-instance-editor] [role="alert"]').text()).toBe(
      'plugin refused config'
    )
  })

  it('checks the dry-run body and channel before sending', async () => {
    vi.mocked(pluginApi.dryRunRequestPlugins).mockResolvedValue({
      steps: [],
      body: {},
    } as never)
    const page = await mountPage()
    await button(page, 'Dry run').trigger('click')
    await flushPromises()
    const dialog = page.get('[data-dry-run-dialog]')
    const body = dialog.get('textarea')
    await body.setValue('[1, 2]')
    await button(page, 'Run').trigger('click')
    expect(dialog.text()).toContain('The request body must be a JSON object')

    await body.setValue('{"model":"claude-sonnet-4-5","messages":[]}')
    await dialog.get('input[inputmode="numeric"]').setValue('0')
    await button(page, 'Run').trigger('click')
    expect(dialog.text()).toContain(
      'The channel ID must be a positive whole number'
    )
    expect(pluginApi.dryRunRequestPlugins).not.toHaveBeenCalled()

    await dialog.get('input[inputmode="numeric"]').setValue('12')
    await button(page, 'Run').trigger('click')
    await flushPromises()
    const [sent] = vi.mocked(pluginApi.dryRunRequestPlugins).mock.calls[0]!
    expect(sent).toEqual({
      format: 'openai',
      path: '/v1/chat/completions',
      model: 'gpt-4.1-mini',
      channel_id: 12,
      body: { model: 'claude-sonnet-4-5', messages: [] },
    })
    expect(dialog.text()).toContain('No instance ran for this request.')
  })
})
