export type SpreadsheetExportFormat = 'csv' | 'excel'
export type SerializedSpreadsheet = [content: string, mime: string, ext: string]
export interface SpreadsheetExportStream {
  prefix: string
  suffix: string
  appendRow(values: readonly unknown[]): string
  mime: string
  ext: string
}

const DANGEROUS_FORMULA_PREFIX = /^[=+\-@]/

function stripLeadingControlCharacters(value: string): string {
  let index = 0
  while (index < value.length) {
    const code = value.charCodeAt(index)
    if (code > 31 && (code < 127 || code > 159)) break
    index += 1
  }
  return value.slice(index)
}

/** Prevents spreadsheet applications from interpreting untrusted text as a formula. */
export function neutralizeSpreadsheetFormula(value: unknown): string {
  if (typeof value !== 'string') return String(value)

  const normalized = stripLeadingControlCharacters(value)
  return DANGEROUS_FORMULA_PREFIX.test(normalized)
    ? `'${normalized}`
    : normalized
}

function csvRow(values: readonly unknown[]): string {
  return values
    .map(
      (value) => `"${neutralizeSpreadsheetFormula(value).replace(/"/g, '""')}"`
    )
    .join(',')
}

function escapeHtml(value: unknown): string {
  return neutralizeSpreadsheetFormula(value)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

export function serializeSpreadsheet(
  headers: readonly unknown[],
  rows: readonly (readonly unknown[])[],
  format: SpreadsheetExportFormat
): SerializedSpreadsheet {
  const stream = createSpreadsheetExportStream(headers, format)
  return [
    stream.prefix +
      rows.map((row) => stream.appendRow(row)).join('') +
      stream.suffix,
    stream.mime,
    stream.ext,
  ]
}

/** Incremental row serializer for large downloads; callers retain no row array. */
export function createSpreadsheetExportStream(
  headers: readonly unknown[],
  format: SpreadsheetExportFormat
): SpreadsheetExportStream {
  if (format === 'excel') {
    const head = `<tr>${headers.map((header) => `<th>${escapeHtml(header)}</th>`).join('')}</tr>`
    return {
      prefix: `<html><head><meta charset="utf-8"></head><body><table>${head}`,
      suffix: '</table></body></html>',
      appendRow: (row) =>
        `<tr>${row.map((value) => `<td>${escapeHtml(value)}</td>`).join('')}</tr>`,
      mime: 'application/vnd.ms-excel;charset=utf-8',
      ext: 'xls',
    }
  }
  return {
    prefix: '\uFEFF' + csvRow(headers),
    suffix: '',
    appendRow: (row) => '\n' + csvRow(row),
    mime: 'text/csv;charset=utf-8',
    ext: 'csv',
  }
}
