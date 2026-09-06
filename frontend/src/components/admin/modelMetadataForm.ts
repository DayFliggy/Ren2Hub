import {
  MODEL_ENDPOINT_TEMPLATES,
  parseModelEndpoints,
} from '@/utils/modelEndpoints'
export const endpointTemplates: Record<
  string,
  { path: string; method: string }
> = MODEL_ENDPOINT_TEMPLATES

export interface MetadataEndpointRow {
  type: string
  path: string
  method: string
}
export const endpointMethods = [
  'GET',
  'POST',
  'PUT',
  'PATCH',
  'DELETE',
  'HEAD',
  'OPTIONS',
]

export function parseMetadataEndpoints(source: string): MetadataEndpointRow[] {
  if (!source.trim()) return []
  let value: ReturnType<typeof parseModelEndpoints>
  try {
    value = parseModelEndpoints(source)
  } catch (cause) {
    throw new Error(
      cause instanceof SyntaxError
        ? 'endpointInvalidJson'
        : cause instanceof Error && cause.message === 'duplicateEndpoint'
          ? 'endpointDuplicate'
          : 'endpointInvalidField',
      { cause }
    )
  }
  // The list API also returns inferred endpoint type arrays for unconfigured models.
  if (Array.isArray(value)) {
    const rows = value.map((type) => ({
      type,
      ...(endpointTemplates[type] ?? { path: '', method: 'POST' }),
    }))
    return rows
  }
  return Object.entries(value).map(([type, endpoint]) => {
    return { type, path: endpoint.path, method: endpoint.method }
  })
}

export function serializeMetadataEndpoints(
  rows: MetadataEndpointRow[]
): string {
  const used = new Set<string>()
  const entries = rows.map(({ type, path, method }) => {
    const key = type.trim()
    const pathname = path.trim()
    if (used.has(key)) throw new Error('endpointDuplicate')
    used.add(key)
    return [key, { path: pathname, method }]
  })
  try {
    return entries.length
      ? JSON.stringify(
          parseModelEndpoints(Object.fromEntries(entries)),
          null,
          2
        )
      : ''
  } catch (cause) {
    throw new Error(
      cause instanceof Error && cause.message === 'duplicateEndpoint'
        ? 'endpointDuplicate'
        : 'endpointInvalidField',
      { cause }
    )
  }
}

export function parseMetadataTags(value: string): string[] {
  if (!value.trim()) return []
  if (value.trim().startsWith('[')) {
    let parsed: unknown
    try {
      parsed = JSON.parse(value)
    } catch (cause) {
      throw new Error('invalidTags', { cause })
    }
    if (
      !Array.isArray(parsed) ||
      !parsed.every((item) => typeof item === 'string')
    )
      throw new Error('invalidTags')
    return parsed
  }
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

export async function runMetadataBatch<T extends { id: number }>(
  rows: T[],
  action: (row: T) => Promise<unknown>
) {
  const succeeded: number[] = []
  const failed: { row: T; message: string }[] = []
  for (const row of rows) {
    try {
      await action(row)
      succeeded.push(row.id)
    } catch (cause) {
      failed.push({
        row,
        message: cause instanceof Error ? cause.message : String(cause),
      })
    }
  }
  return { succeeded, failed }
}
