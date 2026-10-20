import { describe, expect, it, vi } from 'vitest'
import {
  isMetadataSourceChanged,
  metadataApi,
  parseModelMetadata,
  parsePrefillGroup,
  parseSyncPreview,
} from '../adminManagement'
import { ApiError } from '../types'
import { appendModelNames } from '@/utils/modelEndpoints'

const client = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('../client', () => ({ api: client }))

describe('admin management contracts', () => {
  it('preserves enriched model fields and missing timestamps', () => {
    const row = parseModelMetadata({
      id: 1,
      model_name: 'gpt-test',
      vendor_id: 2,
      status: 1,
      sync_official: 0,
      name_rule: 1,
      bound_channels: [{ name: 'primary', type: 1 }],
      enable_groups: ['fast'],
      matched_models: ['gpt-test-1'],
      matched_count: 1,
      quota_types: [0, 1],
      created_time: 100,
    })
    expect(row).toMatchObject({
      matched_count: 1,
      matched_models: ['gpt-test-1'],
      quota_types: [0, 1],
      created_time: 100,
      updated_time: null,
      sync_official: 0,
    })
    expect(() => parseModelMetadata({ ...row, quota_types: ['0'] })).toThrow()
  })
  it('normalizes legacy model/tag arrays and endpoint JSON strings', () => {
    for (const type of ['model', 'tag']) {
      for (const items of [['a', 'b'], '["a","b"]'])
        expect(
          parsePrefillGroup({ id: 1, name: 'fast', type, items }).items
        ).toEqual(['a', 'b'])
    }
    const endpoint = {
      openai: { path: '/v1/chat/completions', method: 'POST' },
    }
    expect(
      JSON.parse(
        parsePrefillGroup({
          id: 2,
          name: 'endpoints',
          type: 'endpoint',
          items: JSON.stringify(endpoint),
        }).items as string
      )
    ).toEqual(endpoint)
    expect(
      JSON.parse(
        parsePrefillGroup({
          id: 2,
          name: 'endpoints',
          type: 'endpoint',
          items: ['openai'],
        }).items as string
      )
    ).toEqual(['openai'])
  })
  it('rejects malformed groups and sync fields without an empty fallback', () => {
    for (const items of ['{', '{"openai":{"path":true,"method":"POST"}}'])
      expect(() =>
        parsePrefillGroup({ id: 1, name: 'bad', type: 'endpoint', items })
      ).toThrow()
    expect(() =>
      parsePrefillGroup({ id: 1, name: 'bad', type: 'model', items: '{}' })
    ).toThrow()
    expect(() =>
      parseSyncPreview({
        missing: [1],
        conflicts: [],
        source: { version: 'v1' },
      })
    ).toThrow()
  })
  it('requires the preview catalog version and sends it with the apply plan', async () => {
    const source = { locale: 'en', version: 'catalog-v1' }
    expect(parseSyncPreview({ missing: ['a'], conflicts: [], source })).toEqual(
      { version: 'catalog-v1', missing: ['a'], conflicts: [] }
    )
    for (const bad of [undefined, {}, { version: '' }])
      expect(() =>
        parseSyncPreview({ missing: [], conflicts: [], source: bad })
      ).toThrow()

    client.post.mockResolvedValueOnce({
      created_models: 1,
      updated_models: 0,
      created_vendors: 0,
      skipped_models: [],
    })
    const plan = {
      source_version: 'catalog-v1',
      create_models: ['a'],
      overwrite: [
        { model_name: 'b', fields: ['description'], local_version: 'local-v1' },
      ],
    }
    await metadataApi.sync('official', 'en', plan)
    expect(client.post).toHaveBeenCalledWith('/api/models/sync_upstream', {
      source: 'official',
      locale: 'en',
      ...plan,
    })
  })
  it('recognizes only the catalog-changed conflict', () => {
    const changed = (status: number, code: string) =>
      isMetadataSourceChanged(new ApiError('x', { status, code }))
    expect(changed(409, 'METADATA_SOURCE_CHANGED')).toBe(true)
    expect(changed(409, 'AUTH_SESSION_CHANGED')).toBe(false)
    expect(changed(400, 'METADATA_SOURCE_CHANGED')).toBe(false)
    expect(isMetadataSourceChanged(new Error('METADATA_SOURCE_CHANGED'))).toBe(
      false
    )
  })
  it('normalizes endpoint formats already accepted by the backend', () => {
    const group = parsePrefillGroup({
      id: 1,
      name: 'legacy endpoints',
      type: 'endpoint',
      items: JSON.stringify({
        openai: '/v1/chat/completions',
        anthropic: { path: '/v1/messages' },
        'openai-response': { path: '/v1/responses', method: 'post' },
      }),
    })
    expect(JSON.parse(group.items as string)).toEqual({
      openai: { path: '/v1/chat/completions', method: 'POST' },
      anthropic: { path: '/v1/messages', method: 'POST' },
      'openai-response': { path: '/v1/responses', method: 'POST' },
    })
  })
  it('appends prefill models once while preserving existing order', () => {
    const existing = ['b', 'a']
    const result = appendModelNames(existing, ['a', 'c', 'c', ' b ', 'd'])
    expect(result).toEqual(['b', 'a', 'c', 'd'])
    expect(appendModelNames(result, ['c', 'a', 'd'])).toEqual(result)
    expect(existing).toEqual(['b', 'a'])
  })
})
