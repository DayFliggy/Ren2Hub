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
import MarketplaceInstallDialog, {
  type MarketplaceInstallTarget,
} from '../components/MarketplaceInstallDialog.vue'
import type { InstallState } from '../lib/marketplace'

vi.mock('../api', () => ({
  activateTaskPlugin: vi.fn(),
  getTaskPlugin: vi.fn(),
  installMarketplacePlugin: vi.fn(),
  previewTaskPlugin: vi.fn(),
}))

const source = 'export const meta = { key: "fixture", version: "2.0.0" }'
// sha256 of source, computed outside the code under test.
const digest =
  '0ad3925c5ff366a8d168ecbbf20c0f7cfc0394462973527b7f0ca6775d66adcc'
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

const readmeText =
  '# Fixture guide\n\nRead the [setup notes](docs/setup.md) and **restart** after install.\n'

function target(
  sha256: string,
  installState: InstallState,
  readme = false
): MarketplaceInstallTarget {
  return {
    source: {
      name: 'Fixture source',
      index_url: 'https://plugins.example/index.json',
    },
    plugin: {
      key: 'fixture',
      name: 'Fixture',
      latest: '2.0.0',
      versions: [
        {
          version: '2.0.0',
          path: 'fixture/2.0.0/plugin.js',
          sha256,
          ...(readme ? { readme: { path: 'fixture/2.0.0/README.md' } } : {}),
        },
      ],
    },
    version: '2.0.0',
    installState,
  }
}

async function open(
  sha256: string,
  installState: InstallState,
  previewMeta: Record<string, unknown> = {
    key: 'fixture',
    version: '2.0.0',
    models: ['m1'],
  },
  readme = false
) {
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) =>
    String(input).endsWith('/README.md')
      ? new Response(readmeText)
      : new Response(source)
  )
  vi.mocked(pluginApi.previewTaskPlugin).mockResolvedValue({
    meta: previewMeta,
    unresolved: [],
    source_hash: digest,
  })
  vi.mocked(pluginApi.getTaskPlugin).mockResolvedValue({
    meta: {
      key: 'fixture',
      name: 'Fixture',
      version: '1.0.0',
      apiVersion: 1,
      models: [],
    },
    source: 'export const meta = {}',
    layer: 'override',
  })
  vi.mocked(pluginApi.installMarketplacePlugin).mockResolvedValue({
    plugin: undefined,
    meta: {
      key: 'fixture',
      name: 'Fixture',
      version: '2.0.0',
      apiVersion: 1,
      models: [],
    },
    source,
    layer: 'override',
  })
  wrapper = mount(MarketplaceInstallDialog, {
    props: { target: target(sha256, installState, readme) },
    global: { plugins: [i18n], stubs: { Teleport: true } },
  })
  // The streamed body takes several macrotasks to drain.
  await vi.waitFor(() =>
    expect(wrapper!.text()).not.toContain('Fetching plugin source')
  )
  await flushPromises()
}

const button = (text: string) =>
  wrapper!.findAll('button').find((item) => item.text().includes(text))!

describe('marketplace install dialog', () => {
  it('blocks the install when the source does not match the index sha256', async () => {
    await open('0'.repeat(64), { status: 'not_installed' })
    expect(wrapper!.text()).toContain('Integrity check failed')
    const install = button('Install and enable')
    expect(install.attributes('disabled')).toBeDefined()
    await install.trigger('click')
    await flushPromises()
    expect(pluginApi.installMarketplacePlugin).not.toHaveBeenCalled()
  })

  it('installs a verified source with the index hash for server re-verification', async () => {
    await open(digest, { status: 'not_installed' })
    expect(wrapper!.text()).toContain('File integrity verified')
    await button('Install and enable').trigger('click')
    await flushPromises()
    expect(pluginApi.installMarketplacePlugin).toHaveBeenCalledWith({
      source,
      sourceSha256: digest,
      marketplaceIndexUrl: 'https://plugins.example/index.json',
      remark: 'Fixture source v2.0.0',
      icon: undefined,
    })
    expect(wrapper!.emitted('installed')).toHaveLength(1)
  })

  it('requires an explicit confirmation before replacing a diverged version', async () => {
    await open(digest, {
      status: 'diverged',
      installedVersion: '1.5.0-local',
      latestVersion: '2.0.0',
    })
    const confirm = () =>
      wrapper!
        .findAll('[role="dialog"]')
        .find((item) =>
          item.text().startsWith('Replace the installed version?')
        )
    await button('Install and enable').trigger('click')
    await flushPromises()
    expect(pluginApi.installMarketplacePlugin).not.toHaveBeenCalled()
    expect(confirm()).toBeDefined()
    const cancel = confirm()!
      .findAll('button')
      .find((item) => item.text() === 'Cancel')!
    await cancel.trigger('click')
    await flushPromises()
    expect(confirm()).toBeUndefined()
    expect(pluginApi.installMarketplacePlugin).not.toHaveBeenCalled()
    await button('Install and enable').trigger('click')
    await flushPromises()
    await confirm()!
      .findAll('button')
      .find((item) => item.text() === 'Replace')!
      .trigger('click')
    await flushPromises()
    expect(pluginApi.installMarketplacePlugin).toHaveBeenCalledOnce()
  })

  it('renders the listed usage guide with links resolved against its page', async () => {
    await open(digest, { status: 'not_installed' }, undefined, true)
    await wrapper!
      .findAll('[role="tab"]')
      .find((item) => item.text() === 'Documentation')!
      .trigger('click')
    const panel = () => wrapper!.get('#marketplace-install-panel')
    await vi.waitFor(() => expect(panel().find('h2').exists()).toBe(true))
    expect(panel().get('h2').text()).toBe('Fixture guide')
    expect(panel().get('strong').text()).toBe('restart')
    const link = panel().get('a')
    expect(link.text()).toBe('setup notes')
    expect(link.attributes('href')).toBe(
      'https://plugins.example/fixture/2.0.0/docs/setup.md'
    )
    expect(fetch).toHaveBeenCalledWith(
      'https://plugins.example/fixture/2.0.0/README.md',
      expect.anything()
    )
  })

  it('blocks a verified source when its metadata identity differs from the index', async () => {
    await open(
      digest,
      { status: 'not_installed' },
      { key: 'other-plugin', version: '2.0.0' }
    )
    expect(wrapper!.text()).toContain('does not match the marketplace index')
    expect(button('Install and enable').attributes('disabled')).toBeDefined()
    expect(pluginApi.installMarketplacePlugin).not.toHaveBeenCalled()
  })
})
