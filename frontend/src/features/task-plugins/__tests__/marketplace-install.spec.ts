import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

import * as pluginApi from '../api'
import {
  downloadMarketplaceRelease,
  installMarketplaceRelease,
  MarketplaceActivationError,
  MarketplaceInstallError,
} from '../lib/marketplace-install'
import { computeSourceSha256 } from '../lib/plugin-url'
import type {
  MarketplaceIndexVersion,
  MarketplacePlugin,
  MarketplaceSource,
} from '../types'

vi.mock('../api', () => ({
  activateTaskPlugin: vi.fn(),
  installMarketplacePlugin: vi.fn(),
  previewTaskPlugin: vi.fn(),
}))

const validSource =
  'export const meta = { key: "alpha", name: "Alpha", version: "2.0.0", apiVersion: 1 }'
let validHash = ''

const source: MarketplaceSource = {
  name: 'Official',
  index_url: 'https://plugins.example/index.json',
}
let plugin: MarketplacePlugin
let entry: MarketplaceIndexVersion

beforeAll(async () => {
  validHash = (await computeSourceSha256(validSource))!
  plugin = {
    key: 'alpha',
    name: 'Alpha',
    latest: '2.0.0',
    versions: [{ version: '2.0.0', path: 'alpha/2.0.0.js', sha256: validHash }],
  }
  entry = plugin.versions[0]!
})

beforeEach(() => {
  vi.clearAllMocks()
})

describe('marketplace release download and install guards', () => {
  it('rejects paths leaving the HTTPS index origin before issuing any network request', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch')
    await expect(
      downloadMarketplaceRelease(source, plugin, {
        ...entry,
        path: 'https://other.example/evil.js',
      })
    ).rejects.toMatchObject({
      name: 'MarketplaceInstallError',
      reason: 'invalid_path',
    })
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('blocks integrity mismatch, unsupported API version, and metadata identity mismatch before upload', async () => {
    await expect(
      installMarketplaceRelease(source, plugin, entry, {
        text: validSource,
        digest: '0'.repeat(64),
        icon: null,
      })
    ).rejects.toEqual(new MarketplaceInstallError('integrity_mismatch'))

    await expect(
      installMarketplaceRelease(
        source,
        plugin,
        { ...entry, minApiVersion: 2 },
        { text: validSource, digest: validHash, icon: null }
      )
    ).rejects.toEqual(new MarketplaceInstallError('unsupported_api'))

    vi.mocked(pluginApi.previewTaskPlugin).mockResolvedValue({
      meta: { key: 'other-plugin', version: '2.0.0' },
      unresolved: [],
      source_hash: validHash,
    })
    await expect(
      installMarketplaceRelease(source, plugin, entry, {
        text: validSource,
        digest: validHash,
        icon: null,
      })
    ).rejects.toEqual(new MarketplaceInstallError('identity_mismatch'))

    expect(pluginApi.installMarketplacePlugin).not.toHaveBeenCalled()
  })

  it('notifies upload before activation and distinguishes activation errors', async () => {
    const onUploaded = vi.fn()
    vi.mocked(pluginApi.previewTaskPlugin).mockResolvedValue({
      meta: { key: 'alpha', version: '2.0.0' },
      unresolved: [],
      source_hash: validHash,
    })
    vi.mocked(pluginApi.installMarketplacePlugin).mockResolvedValue({
      plugin: {
        id: 2,
        key: 'alpha',
        api_version: 1,
        version: '2.0.0',
        source: validSource,
        source_hash: validHash,
        enabled: true,
        active: false,
        factory: false,
        created_at: 1,
        remark: 'Official v2.0.0',
      },
      meta: {
        key: 'alpha',
        name: 'Alpha',
        version: '2.0.0',
        apiVersion: 1,
        models: [],
      },
      source: validSource,
      layer: 'override',
    })
    vi.mocked(pluginApi.activateTaskPlugin).mockRejectedValue(
      new Error('route conflict')
    )

    await expect(
      installMarketplaceRelease(
        source,
        plugin,
        entry,
        { text: validSource, digest: validHash, icon: null },
        { onUploaded }
      )
    ).rejects.toBeInstanceOf(MarketplaceActivationError)
    expect(onUploaded).toHaveBeenCalledOnce()
    expect(pluginApi.installMarketplacePlugin).toHaveBeenCalledWith({
      source: validSource,
      sourceSha256: validHash,
      remark: 'Official v2.0.0',
      icon: undefined,
      marketplaceIndexUrl: source.index_url,
    })
    expect(pluginApi.activateTaskPlugin).toHaveBeenCalledWith('alpha', '2.0.0')
  })
})
