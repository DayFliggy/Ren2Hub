import { describe, expect, it, vi } from 'vitest'

import { collectExportPages } from '@/utils/collectExportPages'

// Mirrors a native list whose total is a capped count: every page is served
// from `rows`, so the helper can only learn the real size by paging.
function pagedSource(rows: number, pageSize: number) {
  return vi.fn(async (page: number) => {
    const start = (page - 1) * pageSize
    const end = Math.min(rows, start + pageSize)
    const items = Array.from(
      { length: Math.max(0, end - start) },
      (_, i) => start + i
    )
    return { items, total: Math.min(rows, 10_000) }
  })
}

describe('collectExportPages', () => {
  it('reports no truncation when exactly maxRows rows exist', async () => {
    const fetchPage = pagedSource(10_000, 100)
    const result = await collectExportPages(fetchPage, 100, 10_000)
    expect(result.items).toHaveLength(10_000)
    expect(result.truncated).toBe(false)
    expect(fetchPage).toHaveBeenCalledTimes(101)
  })

  it('reports truncation when a row exists past maxRows', async () => {
    const fetchPage = pagedSource(10_001, 100)
    const result = await collectExportPages(fetchPage, 100, 10_000)
    expect(result.items).toHaveLength(10_000)
    expect(result.items.at(-1)).toBe(9_999)
    expect(result.truncated).toBe(true)
  })

  it('stops at the first short page', async () => {
    const fetchPage = pagedSource(250, 100)
    const result = await collectExportPages(fetchPage, 100, 10_000)
    expect(result.items).toEqual(Array.from({ length: 250 }, (_, i) => i))
    expect(result.truncated).toBe(false)
    expect(fetchPage).toHaveBeenCalledTimes(3)
  })

  it('returns an empty export without probing past the first page', async () => {
    const fetchPage = pagedSource(0, 100)
    await expect(collectExportPages(fetchPage, 100, 10_000)).resolves.toEqual({
      items: [],
      truncated: false,
    })
    expect(fetchPage).toHaveBeenCalledTimes(1)
  })

  it('fails the export when the truncation probe fails', async () => {
    const source = pagedSource(10_000, 100)
    const fetchPage = vi.fn(async (page: number) => {
      if (page === 101) throw new DOMException('Aborted', 'AbortError')
      return source(page)
    })
    await expect(collectExportPages(fetchPage, 100, 10_000)).rejects.toThrow(
      'Aborted'
    )
  })

  it('rejects limits that are not whole pages and oversized pages', async () => {
    await expect(
      collectExportPages(pagedSource(1, 100), 100, 150)
    ).rejects.toThrow()
    const oversized = vi.fn(async () => ({ items: [1, 2, 3] }))
    await expect(collectExportPages(oversized, 2, 10)).rejects.toThrow()
  })
})
