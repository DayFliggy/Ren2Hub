import { afterEach, describe, expect, it } from 'vitest'

import { getCanvasTheme } from '@/canvas/theme'

afterEach(() => {
  document.documentElement.removeAttribute('style')
})

describe('canvas theme', () => {
  it('resolves night structure from semantic CSS tokens', () => {
    const root = document.documentElement
    root.style.setProperty('--page-background', '#111827')
    root.style.setProperty('--surface-container-lowest', '#0f172a')
    root.style.setProperty('--surface-container', '#1f2937')
    root.style.setProperty('--accent', '#facc15')
    root.style.setProperty('--signal', 'rgb(96 165 250)')
    root.style.setProperty('--glow', '#6ee7b7')

    const theme = getCanvasTheme('dark')

    expect(theme.backgroundTop).toBe('#0f172a')
    expect(theme.backgroundBottom).toBe('#111827')
    expect(theme.nodeSurface).toBe('#1f2937')
    expect(theme.mapHighlight).toBe('#facc15')
    expect(theme.mapLand).toBe('#60a5fa')
    expect(theme.mapRipple).toBe('#6ee7b7')
  })

  it('falls back instead of leaking an unsupported colour into canvas math', () => {
    document.documentElement.style.setProperty('--accent', 'not-a-color')

    expect(getCanvasTheme('dark').accent).toBe('#e4c06b')
  })

  it('resolves day structure from CSS tokens like night does', () => {
    const root = document.documentElement
    root.style.setProperty('--accent', '#000000')
    root.style.setProperty('--canvas-map-ocean', 'rgb(1 2 3)')
    root.style.setProperty('--surface-container', '#123456')

    const theme = getCanvasTheme('light')

    expect(theme.accent).toBe('#000000')
    expect(theme.mapOcean).toBe('#010203')
    expect(theme.hubSurface).toBe('#123456')
    // Vendor logos keep their brand colours in both themes.
    expect(theme.modelColorOverrides.claude).toBe('#C2620A')
  })

  it('falls back to the Desert Ledger palette when day tokens are missing', () => {
    document.documentElement.style.setProperty('--accent', 'not-a-color')

    const theme = getCanvasTheme('light')

    expect(theme.accent).toBe('#d8984c')
    expect(theme.backgroundBottom).toBe('#ece6d4')
  })
})
