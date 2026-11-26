import type { ChartPalette } from './palette'

/**
 * Dual-mood option fragments for ECharts.
 *
 * Day (Desert Ledger): hand-drawn ledger feel — no smoothing, visible circle
 * markers, dashed grid ruling like a paper ledger.
 * Night (B1 Studio): elegant Material — smooth lines with a soft golden glow,
 * hairline solid grid, quiet surfaces.
 *
 * Charts consume these via spreads inside their option builders so each chart
 * keeps its own data/axis specifics.
 */

export interface LineMoodOptions {
  /** series-level props for line charts */
  line: {
    smooth: boolean | number
    symbol: string
    symbolSize: number
    lineStyle: {
      width: number
      shadowBlur?: number
      shadowColor?: string
    }
  }
  /** splitLine style for value axes */
  splitLine: {
    lineStyle: {
      type: 'dashed' | 'solid' | number[]
      color: string
    }
  }
  /** bar series itemStyle borderRadius (top corners) */
  barRadius: number[]
}

export function lineMood(p: ChartPalette): LineMoodOptions {
  if (p.isDark) {
    return {
      line: {
        smooth: true,
        symbol: 'none',
        symbolSize: 0,
        lineStyle: { width: 2, shadowBlur: 8, shadowColor: p.lineGlow },
      },
      splitLine: {
        lineStyle: { type: 'solid', color: p.chartGrid },
      },
      barRadius: [4, 4, 0, 0],
    }
  }
  return {
    line: {
      smooth: false,
      symbol: 'circle',
      symbolSize: 5,
      lineStyle: { width: 2.5, shadowBlur: 0, shadowColor: 'transparent' },
    },
    splitLine: {
      lineStyle: { type: [7, 3, 2, 5], color: p.borderSubtle },
    },
    barRadius: [3, 2, 0, 0],
  }
}

/**
 * Area gradient stops for filled line charts: day keeps the flat soft wash,
 * night fades a glowing accent from top to transparent.
 */
export function areaGradient(
  p: ChartPalette,
  color: string
): { offset: number; color: string }[] {
  if (p.isDark) {
    return [
      { offset: 0, color: color + '3d' },
      { offset: 1, color: color + '00' },
    ]
  }
  return [
    { offset: 0, color: color + '2e' },
    { offset: 1, color: color + '05' },
  ]
}

/**
 * Shared ECharts tooltip chrome aligned with the overlay surface tier
 * (--surface-overlay / --overlay-border) so popovers rise above card containers
 * in both Desert Ledger and B1 Studio.
 */
export function tooltipStyle(p: ChartPalette, scale = 1) {
  return {
    backgroundColor: p.surfaceOverlay,
    borderColor: p.overlayBorder,
    borderWidth: 1,
    textStyle: { color: p.textPrimary, fontSize: 12 * scale },
    extraCssText: 'box-shadow: var(--overlay-shadow);',
  }
}
