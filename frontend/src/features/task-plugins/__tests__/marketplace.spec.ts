import { describe, expect, it } from 'vitest'

import {
  deriveInstallState,
  parseMarketplaceIndex,
  replaceConfirmationReason,
  resolvePluginSourceUrl,
} from '../lib/marketplace'
import {
  parsePluginMetaPreview,
  resolvePluginMetaPreview,
} from '../lib/plugin-meta-preview'
import type { MarketplacePlugin, TaskPluginListItem } from '../types'

const plugin: MarketplacePlugin = {
  key: 'fixture',
  name: 'Fixture',
  latest: '2.0.0',
  // Index order is newest first.
  versions: [
    { version: '2.0.0', path: 'fixture/2.0.0/plugin.js' },
    { version: '1.0.0', path: 'fixture/1.0.0/plugin.js' },
  ],
}

function installed(version: string): TaskPluginListItem[] {
  return [
    {
      meta: {
        key: 'fixture',
        name: 'Fixture',
        version,
        apiVersion: 1,
        models: [],
      },
      source: 'override',
      enabled: true,
      active: true,
      source_hash: '',
      remark: '',
      runtime_status: 'registered',
      channel_count: 0,
      in_flight_count: 0,
    },
  ]
}

describe('marketplace install state', () => {
  it('derives every install state against the installed plugins', () => {
    expect(deriveInstallState(plugin, [])).toEqual({ status: 'not_installed' })
    expect(deriveInstallState(plugin, installed('2.0.0'))).toEqual({
      status: 'up_to_date',
      installedVersion: '2.0.0',
    })
    expect(deriveInstallState(plugin, installed('1.0.0'))).toEqual({
      status: 'upgradable',
      installedVersion: '1.0.0',
      latestVersion: '2.0.0',
    })
    // A locally uploaded version the index never listed is not an upgrade.
    expect(deriveInstallState(plugin, installed('1.5.0-local'))).toEqual({
      status: 'diverged',
      installedVersion: '1.5.0-local',
      latestVersion: '2.0.0',
    })
  })

  it('asks for confirmation only before a diverged or downgrade install', () => {
    const reason = (version: string, target: string) =>
      replaceConfirmationReason(
        plugin,
        deriveInstallState(plugin, installed(version)),
        target
      )
    expect(
      replaceConfirmationReason(plugin, { status: 'not_installed' }, '1.0.0')
    ).toBeNull()
    expect(reason('1.0.0', '2.0.0')).toBeNull()
    expect(reason('2.0.0', '2.0.0')).toBeNull()
    expect(reason('2.0.0', '1.0.0')).toBe('downgrade')
    expect(reason('1.5.0-local', '2.0.0')).toBe('diverged')
  })
})

describe('marketplace index boundary', () => {
  it('resolves sources only to HTTPS on the index origin', () => {
    const index = 'https://plugins.example/index.json'
    expect(resolvePluginSourceUrl(index, 'a/plugin.js')).toBe(
      'https://plugins.example/a/plugin.js'
    )
    expect(resolvePluginSourceUrl(index, '//other.example/a.js')).toBeNull()
    expect(
      resolvePluginSourceUrl(index, 'https://u:p@plugins.example/a.js')
    ).toBeNull()
    expect(
      resolvePluginSourceUrl('http://plugins.example/index.json', 'a.js')
    ).toBeNull()
  })

  it('keeps unhashed versions visible and drops other kinds and remote icons', () => {
    const index = parseMarketplaceIndex({
      indexVersion: 1,
      plugins: [
        {
          key: 'unhashed',
          icon: 'https://tracker.example/pixel.png',
          versions: [{ version: '1', path: 'a.js' }],
        },
        {
          key: 'theme',
          versions: [{ version: '1', path: 'b.js', kind: 'ui' }],
        },
      ],
    })
    expect(index.plugins.map((item) => item.key)).toEqual(['unhashed'])
    expect(index.plugins[0]!.versions[0]!.sha256).toBeUndefined()
    expect(index.plugins[0]!.icon).toBeUndefined()
    expect(() => parseMarketplaceIndex({ indexVersion: 2 })).toThrow(
      'unsupported indexVersion'
    )
  })
})

describe('local metadata preview', () => {
  it('separates missing, unresolved and malformed fields', () => {
    const preview = parsePluginMetaPreview(
      'export const meta = { models: ["m1"], baseUrl: "https://upstream.example", channelTypes: ["not-a-number"], allowedHosts: getHosts() };'
    )
    expect(preview.status).toBe('partial')
    expect(preview.fields.models).toEqual({
      state: 'value',
      value: ['m1'],
      origin: 'source',
    })
    expect(preview.fields.auth).toEqual({ state: 'missing', origin: 'source' })
    expect(preview.fields.allowedHosts).toEqual({ state: 'unknown' })
    expect(preview.fields.channelTypes).toEqual({ state: 'unknown' })
    expect(
      parsePluginMetaPreview('export const meta = getMeta();').status
    ).toBe('unavailable')
  })

  it('falls back to index hints only for unreadable source fields', () => {
    const fields = resolvePluginMetaPreview(
      { ...plugin, models: ['from-index'] },
      plugin.versions[0],
      parsePluginMetaPreview('export const meta = {models: getModels()};')
    )
    expect(fields.models).toEqual({
      state: 'value',
      value: ['from-index'],
      origin: 'index',
    })
    // A field the source omits stays "not declared", never an index hint.
    expect(fields.baseUrl).toEqual({ state: 'missing', origin: 'source' })
  })
})
