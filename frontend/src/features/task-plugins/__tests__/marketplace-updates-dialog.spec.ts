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
import MarketplacePanel from '../components/MarketplacePanel.vue'
import {
  COMMUNITY_MARKETPLACE_INDEX_URL,
  DEFAULT_MARKETPLACE_INDEX_URL,
} from '../lib/marketplace'
import { computeSourceSha256 } from '../lib/plugin-url'
import type {
  MarketplaceIndex,
  MarketplacePlugin,
  MarketplaceSource,
  TaskPluginListItem,
} from '../types'

vi.mock('../api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('../api')>()
  return {
    ...actual,
    listMarketplaceSources: vi.fn(),
    listTaskPlugins: vi.fn(),
    getTaskPlugin: vi.fn(),
    installMarketplacePlugin: vi.fn(),
    activateTaskPlugin: vi.fn(),
  }
})

const sourceCode = (key: string, version: string) =>
  `export const meta = { key: "${key}", name: "${key}", version: "${version}", apiVersion: 1 }\n`
async function sha256(value: string): Promise<string> {
  const digest = await computeSourceSha256(value)
  if (!digest) throw new Error('WebCrypto subtle SHA-256 is unavailable')
  return digest
}

let wrapper: VueWrapper | undefined

beforeAll(async () => {
  await loadMessageDomain('console')
})

beforeEach(async () => {
  vi.clearAllMocks()
  await setLocale('en')
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.unstubAllGlobals()
})

async function openMarket(
  keys: string[],
  options: {
    source?: MarketplaceSource
    locale?: 'en' | 'zh-CN'
  } = {}
) {
  if (options.locale) await setLocale(options.locale)
  const source: MarketplaceSource = options.source ?? {
    name: 'Official',
    index_url: DEFAULT_MARKETPLACE_INDEX_URL,
  }
  const plugins: MarketplacePlugin[] = await Promise.all(
    keys.map(async (key) => ({
      key,
      name: key,
      name_i18n: { 'zh-CN': `${key} 插件`, en: `${key} Plugin` },
      latest: '2.0.0',
      versions: await Promise.all(
        ['2.0.0', '1.0.0'].map(async (version) => ({
          version,
          path: `${key}/${version}.js`,
          sha256: await sha256(sourceCode(key, version)),
        }))
      ),
    }))
  )
  const index: MarketplaceIndex = {
    name: 'Official catalog',
    indexVersion: 1,
    plugins,
  }
  const installed: TaskPluginListItem[] = await Promise.all(
    keys.map(async (key) => ({
      meta: {
        key,
        name: key,
        version: '1.0.0',
        apiVersion: 1,
        models: null,
        fetchMode: 'per_task',
      },
      enabled: key !== 'disabled',
      active: true,
      source_hash: await sha256(sourceCode(key, '1.0.0')),
      source: 'override',
      runtime_status: 'registered',
      remark: '',
      in_flight_count: 0,
      channel_count: 0,
    }))
  )

  vi.mocked(pluginApi.listMarketplaceSources).mockResolvedValue([source])
  vi.mocked(pluginApi.listTaskPlugins).mockImplementation(async () => installed)
  vi.mocked(pluginApi.getTaskPlugin).mockImplementation(
    async (key: string) => ({
      meta: { key, name: key, version: '1.0.0', apiVersion: 1, models: null },
      source: sourceCode(key, '1.0.0'),
      layer: 'override',
    })
  )

  const normalFetch = async (input: RequestInfo | URL) => {
    const url = String(input)
    if (url === source.index_url) return Response.json(index)
    const file = plugins.find((item) => url.endsWith(`/${item.key}/2.0.0.js`))
    return file
      ? new Response(sourceCode(file.key, '2.0.0'))
      : new Response('', { status: 404 })
  }
  const fetchMock = vi.fn(normalFetch)
  vi.stubGlobal('fetch', fetchMock)

  wrapper = mount(MarketplacePanel, {
    props: { installed },
    global: { plugins: [i18n], stubs: { Teleport: true } },
    attachTo: document.body,
  })
  await vi.waitFor(() =>
    expect(wrapper!.findAll('[data-marketplace-plugin-card]').length).toBe(
      keys.length
    )
  )
  await flushPromises()
  return { source, index, installed, fetchMock, normalFetch }
}

const findButton = (text: string) =>
  wrapper!.findAll('button').find((btn) => btn.text().includes(text))
const bulkDialog = () => wrapper!.get('[role="dialog"]')

describe('marketplace bulk updates dialog', () => {
  it('opens a single review for the source, localizes source/plugin names, and lets the admin review diffs without installing', async () => {
    const { fetchMock, source } = await openMarket(
      ['alpha', 'disabled', 'provider'],
      { locale: 'zh-CN' }
    )
    const openBtn = findButton('更新全部（2）')
    expect(openBtn).toBeDefined()
    await openBtn!.trigger('click')
    await flushPromises()

    const dialogs = wrapper!.findAll('[role="dialog"]')
    expect(dialogs).toHaveLength(1)
    expect(dialogs[0]!.text()).toContain('更新已安装插件')
    expect(dialogs[0]!.text()).toContain('来自 官方 的更新')
    expect(dialogs[0]!.text()).toContain('alpha 插件')
    expect(dialogs[0]!.text()).toContain('provider 插件')
    expect(
      fetchMock.mock.calls.every(([url]) => String(url) === source.index_url)
    ).toBe(true)

    const skippedToggle = findButton('已跳过（1）')!
    expect(skippedToggle.attributes('aria-expanded')).toBe('false')
    await skippedToggle.trigger('click')
    expect(skippedToggle.attributes('aria-expanded')).toBe('true')
    expect(wrapper!.text()).toContain('插件已停用')

    const reviewBtn = wrapper!.get(
      'button[aria-label="查看「alpha 插件」的变更"]'
    )
    await reviewBtn.trigger('click')
    await flushPromises()

    const nestedDialogs = wrapper!.findAll('[role="dialog"]')
    expect(nestedDialogs).toHaveLength(2)
    const reviewDialog = nestedDialogs[1]!
    expect(reviewDialog.text()).toContain('alpha 插件')
    expect(reviewDialog.text()).not.toContain('安装并启用')
    expect(
      reviewDialog
        .findAll('[role="tab"]')
        .find((tab) => tab.text() === '更新日志')
        ?.attributes('aria-selected')
    ).toBe('true')
    expect(reviewDialog.find('[aria-label="选择版本"]').exists()).toBe(false)

    await reviewDialog
      .findAll('[role="tab"]')
      .find((tab) => tab.text() === '版本差异')!
      .trigger('click')
    await vi.waitFor(() =>
      expect(reviewDialog.text()).toContain('version: "2.0.0"')
    )

    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper!.findAll('[role="dialog"]')).toHaveLength(1)

    await findButton('取消')!.trigger('click')
    await flushPromises()
    expect(wrapper!.findAll('[role="dialog"]')).toHaveLength(0)
    expect(pluginApi.installMarketplacePlugin).not.toHaveBeenCalled()
  })

  it('continues after integrity, identity, and activation failures, reports pricing issues, and emits changed', async () => {
    const { fetchMock, normalFetch, source } = await openMarket([
      'alpha',
      'beta',
      'disabled',
      'gamma',
    ])
    await findButton('Update all (3)')!.trigger('click')
    await flushPromises()

    let finishAlpha!: (value: Response) => void
    const pendingAlpha = new Promise<Response>((resolve) => {
      finishAlpha = resolve
    })
    fetchMock.mockImplementation(async (url) =>
      String(url).endsWith('/alpha/2.0.0.js') ? pendingAlpha : normalFetch(url)
    )
    vi.mocked(pluginApi.installMarketplacePlugin).mockImplementation(
      async (request: { source: string }) => {
        const key = request.source.includes('"beta"') ? 'beta' : 'gamma'
        return {
          meta: {
            key,
            name: key,
            version: '2.0.0',
            apiVersion: 1,
            models: [],
          },
          source: request.source,
          layer: 'override',
          plugin: {
            id: key === 'beta' ? 10 : 11,
            key,
            api_version: 1,
            version: '2.0.0',
            source: request.source,
            source_hash: await sha256(request.source),
            enabled: true,
            active: key === 'gamma',
            factory: false,
            created_at: 1,
            remark: '',
          },
          pricing_issues:
            key === 'gamma'
              ? [
                  {
                    model_name: 'gamma-video',
                    plugin_key: 'gamma',
                    plugin_name: 'gamma',
                    reason: 'unconfigured',
                  },
                ]
              : [],
        }
      }
    )
    vi.mocked(pluginApi.activateTaskPlugin).mockRejectedValue(
      new Error('route conflict')
    )

    await bulkDialog()
      .findAll('button')
      .find((btn) => btn.text().includes('Update all (3)'))!
      .trigger('click')
    await vi.waitFor(() =>
      expect(bulkDialog().find('[role="progressbar"]').exists()).toBe(true)
    )
    expect(bulkDialog().text()).toContain('Queued')
    expect(
      bulkDialog()
        .findAll('button')
        .find((btn) => btn.text().includes('Updating...'))!
        .attributes('disabled')
    ).toBeDefined()

    // Escape must not close the dialog while updates are in flight.
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper!.findAll('[role="dialog"]')).toHaveLength(1)

    finishAlpha(new Response('tampered bytes'))
    await vi.waitFor(() =>
      expect(bulkDialog().text()).toContain('1 updated · 2 failed · 1 skipped')
    )
    expect(bulkDialog().text()).toContain('Integrity check failed')
    expect(bulkDialog().text()).toContain('Updated successfully')
    expect(bulkDialog().text()).toContain('Uploaded, but activation failed.')
    expect(bulkDialog().text()).toContain('route conflict')
    expect(bulkDialog().text()).toContain('gamma-video')
    expect(bulkDialog().text()).toContain('No price set')
    expect(wrapper!.emitted('changed')).toHaveLength(1)
    expect(
      vi
        .mocked(pluginApi.installMarketplacePlugin)
        .mock.calls.map(([request]) => [
          request.source,
          request.sourceSha256,
          request.marketplaceIndexUrl,
        ])
    ).toEqual(
      await Promise.all(
        ['beta', 'gamma'].map(async (key) => [
          sourceCode(key, '2.0.0'),
          await sha256(sourceCode(key, '2.0.0')),
          source.index_url,
        ])
      )
    )
    expect(vi.mocked(pluginApi.activateTaskPlugin).mock.calls).toEqual([
      ['beta', '2.0.0'],
    ])
  })

  it('rechecks disabled, republished, and withdrawn releases before sending any upload', async () => {
    const { index, installed } = await openMarket(['alpha', 'beta', 'gamma'])
    await findButton('Update all (3)')!.trigger('click')
    await flushPromises()

    installed[0] = { ...installed[0]!, enabled: false }
    index.plugins[1]!.versions[0]!.sha256 = await sha256('republished build')
    index.revoked = [{ sha256: await sha256(sourceCode('gamma', '2.0.0')) }]

    await bulkDialog()
      .findAll('button')
      .find((btn) => btn.text().includes('Update all (3)'))!
      .trigger('click')
    await vi.waitFor(() =>
      expect(bulkDialog().text()).toContain('0 updated · 0 failed · 3 skipped')
    )
    expect(bulkDialog().text()).toContain('Plugin is disabled')
    expect(bulkDialog().text()).toContain('This release was withdrawn.')
    expect(bulkDialog().text()).toContain(
      'The installed plugin or the marketplace release changed after review.'
    )
    expect(pluginApi.installMarketplacePlugin).not.toHaveBeenCalled()
    expect(wrapper!.emitted('changed')).toBeUndefined()
  })

  it('explains skipped-only batches without offering an empty update button', async () => {
    await openMarket(['disabled'])
    await findButton('Skipped (1)')!.trigger('click')
    await flushPromises()
    expect(bulkDialog().text()).toContain('Plugin is disabled')
    expect(
      bulkDialog()
        .findAll('button')
        .find((btn) => btn.text().includes('Update all'))
    ).toBeUndefined()
    expect(pluginApi.installMarketplacePlugin).not.toHaveBeenCalled()
  })

  it('keeps the batch retryable when live recheck fails and displays community telemetry disclosure', async () => {
    const { fetchMock } = await openMarket(['alpha'], {
      source: {
        name: 'Community',
        index_url: COMMUNITY_MARKETPLACE_INDEX_URL,
      },
    })
    await findButton('Update all (1)')!.trigger('click')
    await flushPromises()

    expect(bulkDialog().text()).toContain(
      'When installing from the built-in community marketplace'
    )

    fetchMock.mockResolvedValueOnce(new Response('', { status: 503 }))
    await bulkDialog()
      .findAll('button')
      .find((btn) => btn.text().includes('Update all (1)'))!
      .trigger('click')
    await vi.waitFor(() =>
      expect(bulkDialog().get('[role="alert"]').text()).toContain('503')
    )
    expect(pluginApi.installMarketplacePlugin).not.toHaveBeenCalled()

    vi.mocked(pluginApi.installMarketplacePlugin).mockResolvedValue({
      meta: {
        key: 'alpha',
        name: 'alpha',
        version: '2.0.0',
        apiVersion: 1,
        models: [],
      },
      source: sourceCode('alpha', '2.0.0'),
      layer: 'override',
      plugin: {
        id: 1,
        key: 'alpha',
        api_version: 1,
        version: '2.0.0',
        source: sourceCode('alpha', '2.0.0'),
        source_hash: await sha256(sourceCode('alpha', '2.0.0')),
        enabled: true,
        active: true,
        factory: false,
        created_at: 1,
        remark: '',
      },
    })
    await bulkDialog()
      .findAll('button')
      .find((btn) => btn.text().includes('Update all (1)'))!
      .trigger('click')
    await vi.waitFor(() =>
      expect(bulkDialog().text()).toContain('1 updated · 0 failed · 0 skipped')
    )
    expect(pluginApi.installMarketplacePlugin).toHaveBeenCalledTimes(1)
  })
})
