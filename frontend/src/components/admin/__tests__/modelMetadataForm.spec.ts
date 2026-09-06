import { describe, expect, it, vi } from 'vitest'
import {
  parseMetadataEndpoints,
  serializeMetadataEndpoints,
  runMetadataBatch,
  endpointTemplates,
} from '../modelMetadataForm'

describe('model metadata endpoint editing', () => {
  it('round trips endpoint maps and resolves historical inferred type lists', () => {
    const rows = parseMetadataEndpoints('["openai","jina-rerank"]')
    expect(rows[1].path).toBe('/v1/rerank')
    expect(parseMetadataEndpoints(serializeMetadataEndpoints(rows))).toEqual(
      rows
    )
    expect(endpointTemplates.gemini.path).toBe(
      '/v1beta/models/{model}:generateContent'
    )
  })
  it('rejects malformed JSON, invalid field types, duplicate rows and external URLs', () => {
    expect(() => parseMetadataEndpoints('{')).toThrow('endpointInvalidJson')
    expect(() =>
      parseMetadataEndpoints('{"openai":{"path":4,"method":"POST"}}')
    ).toThrow('endpointInvalidField')
    expect(() =>
      serializeMetadataEndpoints([
        { type: 'x', path: '//external.test/path', method: 'POST' },
      ])
    ).toThrow('endpointInvalidField')
    expect(() =>
      serializeMetadataEndpoints([
        { type: 'x', path: '/first', method: 'POST' },
        { type: ' x ', path: '/second', method: 'POST' },
      ])
    ).toThrow('endpointDuplicate')
  })
  it('retains unknown inferred endpoint types until the user supplies a valid path', () => {
    const rows = parseMetadataEndpoints('["custom-endpoint"]')
    expect(rows[0].type).toBe('custom-endpoint')
    expect(() => serializeMetadataEndpoints(rows)).toThrow(
      'endpointInvalidField'
    )
  })
  it('lets local Compact, Alpha Search and Video models append endpoint overrides', () => {
    const rows = parseMetadataEndpoints(
      '["openai-response-compact","openai-alpha-search","openai-video"]'
    )
    rows.push({ type: 'custom', path: '/v1/responses', method: 'POST' })
    expect(JSON.parse(serializeMetadataEndpoints(rows))).toEqual({
      'openai-response-compact': {
        path: '/v1/responses/compact',
        method: 'POST',
      },
      'openai-alpha-search': { path: '/v1/alpha/search', method: 'POST' },
      'openai-video': { path: '/v1/videos', method: 'POST' },
      custom: { path: '/v1/responses', method: 'POST' },
    })
  })
})

describe('metadata bulk operations', () => {
  it('processes every item once and returns failed rows for retry', async () => {
    const rows = [{ id: 1 }, { id: 2 }, { id: 3 }]
    const action = vi.fn(async (row: { id: number }) => {
      if (row.id === 2) throw new Error('permission denied')
    })
    const result = await runMetadataBatch(rows, action)
    expect(action).toHaveBeenCalledTimes(3)
    expect(result.succeeded).toEqual([1, 3])
    expect(result.failed).toEqual([
      { row: rows[1], message: 'permission denied' },
    ])
    await runMetadataBatch(
      result.failed.map((item) => item.row),
      action
    )
    expect(action).toHaveBeenCalledTimes(4)
    expect(action).toHaveBeenLastCalledWith(rows[1])
  })
})
