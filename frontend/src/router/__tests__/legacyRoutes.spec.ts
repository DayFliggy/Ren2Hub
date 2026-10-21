import { describe, expect, it } from 'vitest'
import { canonicalLegacyLocation, canonicalLegacyPath } from '../legacyRoutes'

describe('native settings routes and historical bookmarks', () => {
  it.each(['dashboard', 'console-content'])(
    'keeps the current content/%s section reachable',
    (section) => {
      const path = `/system-settings/content/${section}`
      expect(canonicalLegacyPath(path, new URLSearchParams())).toBe(path)
      expect(
        canonicalLegacyLocation(`/console${path}`, { source: 'bookmark' })
      ).toEqual({
        path,
        query: { source: 'bookmark' },
      })
    }
  )

  it('still maps the retired settings tab to its original content replacement', () => {
    expect(
      canonicalLegacyPath('/setting', new URLSearchParams({ tab: 'dashboard' }))
    ).toBe('/system-settings/content/console-content')
  })
})
