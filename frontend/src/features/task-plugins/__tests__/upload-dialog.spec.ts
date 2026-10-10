import { mount, type VueWrapper } from '@vue/test-utils'
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
import UploadDialog from '../components/UploadDialog.vue'

vi.mock('../api', () => ({
  activateTaskPlugin: vi.fn(),
  previewTaskPlugin: vi.fn(),
  uploadTaskPlugin: vi.fn(),
}))

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

async function previewSource(source: string, meta: Record<string, unknown>) {
  vi.mocked(pluginApi.previewTaskPlugin).mockResolvedValue({
    meta,
    unresolved: [],
    source_hash: 'fixture-hash',
  })
  wrapper = mount(UploadDialog, {
    global: { plugins: [i18n], stubs: { Teleport: true } },
  })
  const editor = wrapper.get('textarea#task-plugin-source')
  // Typing settles before the server parses the source once.
  await editor.setValue(source.slice(0, 10))
  await editor.setValue(source)
  await vi.waitFor(() =>
    expect(
      wrapper!.find('section[aria-label="Connection settings"]').exists()
    ).toBe(true)
  )
  expect(pluginApi.previewTaskPlugin).toHaveBeenCalledTimes(1)
  expect(pluginApi.previewTaskPlugin).toHaveBeenCalledWith(
    source,
    expect.any(AbortSignal)
  )
  return wrapper.get('section[aria-label="Connection settings"]')
}

describe('upload dialog declaration preview', () => {
  it('lists the request domains the plugin source declares', async () => {
    const connections = await previewSource(
      'export const meta = {models: ["m"], allowedHosts: ["files.example.com", "cdn.example.com:8443"]};',
      {
        models: ['m'],
        allowedHosts: ['files.example.com', 'cdn.example.com:8443'],
      }
    )
    expect(connections.text()).toContain(
      'files.example.com, cdn.example.com:8443'
    )
    expect(connections.text()).not.toContain('From marketplace index')
  })

  it('says no extra domains are declared when the plugin lists none', async () => {
    const connections = await previewSource(
      'export const meta = {models: ["m"]};',
      { models: ['m'] }
    )
    expect(connections.text()).toContain('No extra domains declared')
  })
})
