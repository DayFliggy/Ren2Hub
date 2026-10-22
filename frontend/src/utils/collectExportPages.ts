export interface CollectedExport<T> {
  items: T[]
  truncated: boolean
}

/**
 * Pages through a list endpoint for export. Native list totals can be capped
 * counts, which cannot tell "exactly maxRows" from "more than maxRows", so
 * truncation is decided by probing the first page past maxRows instead.
 */
export async function collectExportPages<T>(
  fetchPage: (page: number) => Promise<{ items: T[] }>,
  pageSize: number,
  maxRows: number
): Promise<CollectedExport<T>> {
  if (
    !Number.isInteger(pageSize) ||
    pageSize <= 0 ||
    !Number.isInteger(maxRows) ||
    maxRows <= 0 ||
    maxRows % pageSize !== 0
  ) {
    throw new Error('Export maxRows must be a positive multiple of pageSize')
  }
  const lastPage = maxRows / pageSize
  const items: T[] = []
  for (let page = 1; page <= lastPage; page++) {
    const rows = await fetchExportPage(fetchPage, page, pageSize)
    items.push(...rows)
    if (rows.length < pageSize) return { items, truncated: false }
  }
  const probe = await fetchExportPage(fetchPage, lastPage + 1, pageSize)
  return { items, truncated: probe.length > 0 }
}

async function fetchExportPage<T>(
  fetchPage: (page: number) => Promise<{ items: T[] }>,
  page: number,
  pageSize: number
): Promise<T[]> {
  const { items } = await fetchPage(page)
  // A larger page would shift every later offset and duplicate or skip rows.
  if (items.length > pageSize) {
    throw new Error(`Export page ${page} exceeded page size ${pageSize}`)
  }
  return items
}
