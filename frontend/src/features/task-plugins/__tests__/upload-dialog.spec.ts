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
import UploadDialog from '../components/UploadDialog.vue'

vi.mock('../api', () => ({
  activateTaskPlugin: vi.fn(),
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

async function previewSource(source: string) {
  wrapper = mount(UploadDialog, {
    global: { plugins: [i18n], stubs: { Teleport: true } },
  })
  const editor = wrapper.get('textarea#task-plugin-source')
  // Typing settles before the source is inspected locally.
  await editor.setValue(source.slice(0, 10))
  await editor.setValue(source)
  await vi.waitFor(() =>
    expect(
      wrapper!.find('section[aria-label="Connection settings"]').exists()
    ).toBe(true)
  )
  return wrapper.get('section[aria-label="Connection settings"]')
}

describe('upload dialog declaration preview', () => {
  it('lists the request domains the plugin source declares', async () => {
    const connections = await previewSource(
      'export const meta = {models: ["m"], allowedHosts: ["files.example.com", "cdn.example.com:8443"]};'
    )
    expect(connections.text()).toContain(
      'files.example.com, cdn.example.com:8443'
    )
    expect(connections.text()).not.toContain('From marketplace index')
  })

  it('says no extra domains are declared when the plugin lists none', async () => {
    const connections = await previewSource(
      'export const meta = {models: ["m"]};'
    )
    expect(connections.text()).toContain('No extra domains declared')
  })
  it('shows dynamic metadata as unknown and still submits native upload for authoritative validation', async () => {
    const source = 'export const meta = buildMeta();'
    await previewSource(source)
    expect(wrapper!.text()).toContain(
      'Some plugin information could not be read'
    )
    vi.mocked(pluginApi.uploadTaskPlugin).mockRejectedValueOnce(
      new Error('Server rejected plugin metadata')
    )
    const upload = wrapper!
      .findAll('button')
      .find((item) => item.text() === 'Upload')!
    expect(upload.attributes('disabled')).toBeUndefined()
    await upload.trigger('click')
    await flushPromises()
    expect(pluginApi.uploadTaskPlugin).toHaveBeenCalledWith({
      source,
      sourceSha256: undefined,
      remark: '',
      icon: undefined,
    })
    expect(wrapper!.text()).toContain('Server rejected plugin metadata')
    expect(
      wrapper!.get<HTMLTextAreaElement>('textarea#task-plugin-source').element
        .value
    ).toBe(source)
  })
})
