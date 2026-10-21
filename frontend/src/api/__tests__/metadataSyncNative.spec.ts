import { describe, expect, it, vi } from 'vitest'
import { metadataApi, parseSyncPreview } from '../adminManagement'

const client = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('../client', () => ({ api: client }))
const candidate = {
  model_name: 'new-model',
  kind: 'create',
  scope: 'site',
  record_version: 'new-v1',
  vendor_to_create: 'Provider',
  fields: [{ field: 'description', local: null, upstream: 'New metadata' }],
}
describe('native metadata sync wire contract', () => {
  it('reads candidates and native record versions without silently producing an empty preview', () => {
    expect(
      parseSyncPreview({
        source: { locale: 'en', version: 'source-v1' },
        candidates: [candidate],
      })
    ).toMatchObject({
      version: 'source-v1',
      locale: 'en',
      candidates: [candidate],
    })
  })
  it('rejects retired or malformed preview shapes', () => {
    for (const candidates of [
      undefined,
      [{ ...candidate, kind: 'unexpected' }],
      [{ ...candidate, record_version: '' }],
    ])
      expect(() =>
        parseSyncPreview({
          source: { locale: 'en', version: 'v1' },
          candidates,
        })
      ).toThrow()
  })
  it('sends native selections and reads array results', async () => {
    const selected = {
      model_name: 'new-model',
      record_version: 'new-v1',
      create: true,
      fields: ['description'],
    }
    const result = {
      created_models: ['new-model'],
      updated_models: [],
      created_vendors: ['Provider'],
    }
    client.post.mockResolvedValueOnce(result)
    await expect(
      metadataApi.sync('en', {
        source_version: 'source-v1',
        selections: [selected],
      })
    ).resolves.toEqual(result)
    expect(client.post).toHaveBeenCalledWith('/api/models/sync_upstream', {
      locale: 'en',
      source_version: 'source-v1',
      selections: [selected],
    })
  })
  it('accepts native null arrays but rejects missing fields and retired result counters', async () => {
    client.post.mockResolvedValueOnce({
      created_models: null,
      updated_models: null,
      created_vendors: null,
    })
    await expect(
      metadataApi.sync('en', { source_version: 'v1', selections: [] })
    ).resolves.toEqual({
      created_models: [],
      updated_models: [],
      created_vendors: [],
    })
    for (const result of [
      {},
      { created_models: 1, updated_models: 0, created_vendors: 0 },
    ]) {
      client.post.mockResolvedValueOnce(result)
      await expect(
        metadataApi.sync('en', { source_version: 'v1', selections: [] })
      ).rejects.toThrow('Invalid API response')
    }
  })
})
