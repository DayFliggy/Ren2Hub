import { describe, expect, it } from 'vitest'

import type { ChartPalette } from '@/charts/palette'
import { areaGradient, lineMood, tooltipStyle } from '@/charts/themePreset'

function palette(isDark: boolean): ChartPalette {
  return {
    accent: '#d8984c',
    signal: '#74765a',
    signalStrong: '#5c5e45',
    support: '#cfaf6b',
    glow: '#7fa463',
    trendCpu: '#224a8d',
    trendMemory: '#7a6fb1',
    trendDisk: '#77324b',
    success: '#64764b',
    warning: '#a87b2a',
    danger: '#9d3017',
    info: '#74765a',
    textPrimary: isDark ? '#eef1f7' : '#38372b',
    textSecondary: '#5c5946',
    textTertiary: '#6c6955',
    borderSubtle: 'rgba(56,55,43,.08)',
    overlayBorder: isDark ? 'rgba(226,232,245,.16)' : 'rgba(192,139,87,.2)',
    chartGrid: 'rgba(152,164,192,.08)',
    surfaceSolid: isDark ? '#262a34' : '#fffdf8',
    surfaceOverlay: isDark ? '#373d4b' : '#fbf6ec',
    isDark,
    lineGlow: isDark ? 'rgba(226,188,85,.35)' : 'transparent',
    series: [],
  }
}

describe('chart mood', () => {
  it('uses a visible, unsmoothed pencil line and irregular ledger grid by day', () => {
    const mood = lineMood(palette(false))

    expect(mood.line).toMatchObject({
      smooth: false,
      symbol: 'circle',
      symbolSize: 5,
      lineStyle: { width: 2.5, shadowBlur: 0, shadowColor: 'transparent' },
    })
    expect(mood.splitLine.lineStyle.type).toEqual([7, 3, 2, 5])
    expect(mood.barRadius).toEqual([3, 2, 0, 0])
    expect(areaGradient(palette(false), '#d8984c')).toEqual([
      { offset: 0, color: '#d8984c2e' },
      { offset: 1, color: '#d8984c05' },
    ])
  })

  it('preserves the smooth glowing B1 Studio chart mood', () => {
    const mood = lineMood(palette(true))

    expect(mood.line).toMatchObject({
      smooth: true,
      symbol: 'none',
      symbolSize: 0,
      lineStyle: {
        width: 2,
        shadowBlur: 8,
        shadowColor: 'rgba(226,188,85,.35)',
      },
    })
    expect(mood.splitLine.lineStyle.type).toBe('solid')
    expect(mood.splitLine.lineStyle.color).toBe('rgba(152,164,192,.08)')
    expect(mood.barRadius).toEqual([4, 4, 0, 0])
  })

  it('elevates chart tooltips onto the overlay surface tier in both themes', () => {
    const dark = palette(true)
    const light = palette(false)

    expect(tooltipStyle(dark, 1.25)).toMatchObject({
      backgroundColor: dark.surfaceOverlay,
      borderColor: dark.overlayBorder,
      textStyle: { color: dark.textPrimary, fontSize: 15 },
    })
    expect(tooltipStyle(light)).toMatchObject({
      backgroundColor: light.surfaceOverlay,
      borderColor: light.overlayBorder,
      textStyle: { color: light.textPrimary, fontSize: 12 },
    })
  })
})
