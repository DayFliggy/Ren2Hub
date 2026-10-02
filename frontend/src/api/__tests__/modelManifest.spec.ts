import { describe, expect, it } from 'vitest'
import {
  parseModelCapabilities,
  parseModelClientConfig,
  parseModelManifest,
} from '../modelManifest'

describe('model manifest boundary', () => {
  it('preserves unknown, explicit empty and false capability facts', () => {
    expect(parseModelCapabilities(undefined, 'test')).toBeNull()
    const card = parseModelCapabilities(
      {
        reasoning_efforts: [],
        input_modalities: null,
        tool_call: false,
        context_limit: 32000,
      },
      'test'
    )
    expect(card).toMatchObject({
      reasoning_efforts: [],
      input_modalities: null,
      tool_call: false,
      context_limit: 32000,
      output_limit: null,
    })
    expect(() =>
      parseModelCapabilities({ tool_call: 'false' }, 'test')
    ).toThrow()
    expect(() => parseModelCapabilities({ context_limit: 0 }, 'test')).toThrow()
  })
  it('requires the manifest contract instead of accepting an unfiltered name list', () => {
    expect(() => parseModelManifest({ models: ['name'] })).toThrow()
    const manifest = parseModelManifest({
      schema_version: 1,
      models: [
        {
          id: 'custom',
          visible: true,
          supported_endpoint_types: ['openai'],
          price_configured: false,
          price_version: '',
        },
      ],
    })
    expect(manifest.models[0]).toMatchObject({
      id: 'custom',
      capabilities: null,
      price_configured: false,
    })
  })
  it('rejects unsafe download paths and unknown export formats', () => {
    const base = {
      format: 'pi',
      filename: 'ren2hub.models.json',
      media_type: 'application/json',
      content: '{}',
      models: ['custom'],
      diagnostics: [{ code: 'prices_omitted' }],
    }
    expect(parseModelClientConfig(base)).toEqual(base)
    expect(() =>
      parseModelClientConfig({ ...base, filename: '../../settings.json' })
    ).toThrow()
    expect(() => parseModelClientConfig({ ...base, format: 'shell' })).toThrow()
  })
})
