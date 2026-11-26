import { describe, expect, it } from 'vitest'

const tailwindSheets = import.meta.glob<string>('../tailwind.css', {
  query: '?raw',
  import: 'default',
  eager: true,
})
const tailwindCss = Object.values(tailwindSheets)[0] ?? ''

const vueSources = import.meta.glob<string>('@/**/*.vue', {
  query: '?raw',
  import: 'default',
  eager: true,
})

/**
 * Guardrails for visual-system invariants documented in docs/THEMES.md:
 * - No Tailwind default blue ring fallback.
 * - No hardcoded hex colors in ConsoleToggle (must flow through tokens.css).
 * - No backdrop-blur on non-nav data popovers, badges, or form footers.
 * - No hardcoded global overlay z-index layers (z-[70..300] / z-index: 70..300)
 *   bypassing --z-topbar / --z-drawer / --z-modal / --z-popover / --z-tooltip.
 * - No inline bg-[var(--surface-muted)] overriding .console-bulk-bar's layered
 *   opaque sticky background on mobile.
 * - No ECharts tooltip painted with surfaceSolid instead of surfaceOverlay.
 */
describe('visual system hygiene guardrails', () => {
  it('routes Tailwind default ring color through --focus-ring instead of raw blue', () => {
    expect(tailwindCss).toContain('--default-ring-color: var(--focus-ring)')
    expect(tailwindCss).not.toContain('#3b82f6')
  })

  it('keeps ConsoleToggle free of hardcoded hex colors', () => {
    const toggleSource =
      vueSources['/src/components/common/ConsoleToggle.vue'] ?? ''
    expect(toggleSource).not.toMatch(/#[0-9a-fA-F]{3,8}\b/)
  })

  it('keeps mobile sticky .console-bulk-bar free of inline translucent background overrides', () => {
    const offenders: string[] = []
    for (const [file, source] of Object.entries(vueSources)) {
      if (
        /class="[^"]*console-bulk-bar[^"]*bg-\[var\(--surface-muted\)\]/.test(
          source
        ) ||
        /class="[^"]*bg-\[var\(--surface-muted\)\][^"]*console-bulk-bar/.test(
          source
        )
      ) {
        offenders.push(file)
      }
    }
    expect(offenders).toEqual([])
  })

  it('uses shared --z-* tokens instead of hardcoded overlay z-index numbers (>= 70)', () => {
    const offenders: string[] = []
    const numericOverlayZ =
      /\bz-\[(?:[7-9]\d|[1-9]\d{2,})\]|z-index:\s*(?:[7-9]\d|[1-9]\d{2,})\b/
    for (const [file, source] of Object.entries(vueSources)) {
      if (numericOverlayZ.test(source)) offenders.push(file)
    }
    expect(offenders).toEqual([])
  })

  it('forbids backdrop-blur on non-nav popovers, badges, menus, and settings footers', () => {
    const allowedBlurFiles = new Set([
      '/src/components/layout/AppNavbar.vue',
      '/src/components/layout/MobileNavDrawer.vue',
      '/src/components/console/ConsoleTopbar.vue',
      '/src/components/console/ConsoleSidebar.vue',
      '/src/components/common/ConsoleModal.vue',
      '/src/components/console/tickets/TicketImageLightbox.vue',
      '/src/views/HomeView.vue',
      // BonusCard optical frosted-glass card is a documented snapshot contract
      '/src/components/console/dashboard/BonusCard.vue',
    ])
    const offenders: string[] = []
    for (const [file, source] of Object.entries(vueSources)) {
      if (allowedBlurFiles.has(file)) continue
      if (/backdrop-blur|backdrop-filter:\s*blur\(/.test(source)) {
        offenders.push(file)
      }
    }
    expect(offenders).toEqual([])
  })

  it('uses surfaceOverlay via tooltipStyle instead of surfaceSolid for ECharts tooltips', () => {
    const offenders: string[] = []
    const tooltipSolid =
      /tooltip\s*:\s*\{[\s\S]{0,220}backgroundColor\s*:\s*\w+\.surfaceSolid\b/
    for (const [file, source] of Object.entries(vueSources)) {
      if (tooltipSolid.test(source)) offenders.push(file)
    }
    expect(offenders).toEqual([])
  })
})
