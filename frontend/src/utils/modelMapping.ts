export interface ModelMappingRow {
  from: string
  to: string
}

export interface ModelMappingError {
  key:
    | 'channels.modelMappingInvalidJson'
    | 'channels.modelMappingNotObject'
    | 'channels.modelMappingValueNotString'
  params?: { key: string }
}

export type ModelMappingParseResult =
  | { ok: true; rows: ModelMappingRow[] }
  | { ok: false; error: ModelMappingError }

// Blank text means no mapping. Anything else must decode the way the relay
// reads model_mapping (map[string]string), matching upstream web validation.
export function parseModelMapping(text: string): ModelMappingParseResult {
  if (!text.trim()) return { ok: true, rows: [] }
  let parsed: unknown
  try {
    parsed = JSON.parse(text)
  } catch {
    return { ok: false, error: { key: 'channels.modelMappingInvalidJson' } }
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
    return { ok: false, error: { key: 'channels.modelMappingNotObject' } }
  const rows: ModelMappingRow[] = []
  for (const [from, to] of Object.entries(parsed)) {
    if (typeof to !== 'string')
      return {
        ok: false,
        error: {
          key: 'channels.modelMappingValueNotString',
          params: { key: from },
        },
      }
    rows.push({ from, to })
  }
  return { ok: true, rows }
}
