import { beforeEach, describe, expect, it, vi } from 'vitest'

import { getCanvasTheme } from '@/canvas/theme'
import { WorldMap } from '@/canvas/WorldMap'

interface PaintedDot {
  x: number
  y: number
  size: number
  color: string
}

/**
 * A 2D context that records the small squares a frame paints, whether they
 * were filled one by one or as a path of rectangles filled together.
 */
function recordingContext() {
  const painted: PaintedDot[] = []
  let path: Omit<PaintedDot, 'color'>[] = []
  const dot = (x: number, y: number, size: number) => ({
    x: x + size / 2,
    y: y + size / 2,
    size,
  })
  const context = {
    fillStyle: '' as unknown,
    createLinearGradient: () => ({ addColorStop() {} }),
    beginPath() {
      path = []
    },
    rect(x: number, y: number, size: number) {
      path.push(dot(x, y, size))
    },
    fill() {
      for (const entry of path)
        painted.push({ ...entry, color: String(context.fillStyle) })
    },
    fillRect(x: number, y: number, size: number) {
      // The background is one gradient rectangle, not a dot.
      if (typeof context.fillStyle !== 'string') return
      painted.push({ ...dot(x, y, size), color: context.fillStyle })
    },
  }
  const proxy = new Proxy(context, {
    get: (target, key) => (key in target ? Reflect.get(target, key) : () => {}),
  })
  return {
    context: proxy as unknown as CanvasRenderingContext2D,
    /** Paints one frame and returns its dots. */
    frame(map: WorldMap, ripples?: Parameters<WorldMap['draw']>[1]) {
      painted.length = 0
      map.draw(proxy as unknown as CanvasRenderingContext2D, ripples)
      return [...painted]
    },
  }
}

function rgb(hex: string): string {
  const value = hex.replace('#', '')
  return [0, 2, 4].map((at) => parseInt(value.slice(at, at + 2), 16)).join(',')
}

const colorOf = (dot: PaintedDot) => /rgba\((\d+,\d+,\d+),/.exec(dot.color)?.[1]
const positions = (dots: PaintedDot[]) =>
  dots.map((dot) => `${dot.x.toFixed(3)},${dot.y.toFixed(3)}`)

describe('world map dots', () => {
  const theme = getCanvasTheme('light')
  const land = rgb(theme.mapLand)
  const highlight = rgb(theme.mapHighlight)
  const ocean = rgb(theme.mapOcean)

  beforeEach(() => {
    vi.spyOn(Math, 'random').mockReturnValue(0.5)
  })

  function createMap(reduced: boolean) {
    const map = new WorldMap(reduced, theme)
    map.setLayout(1440, 900)
    return map
  }

  it('paints every dot once per frame, in the colour of its kind', () => {
    const canvas = recordingContext()
    const map = createMap(true)

    const first = canvas.frame(map)
    const second = canvas.frame(map)

    expect(first.length).toBeGreaterThan(1000)
    expect(new Set(positions(first)).size).toBe(first.length)
    // Without motion the map is still, so a frame repeats the previous one.
    expect(second).toEqual(first)
    // Ocean dots are the small ones; land dots carry the land or star colour.
    const small = first.filter((dot) => dot.size === 0.8)
    const large = first.filter((dot) => dot.size > 0.8)
    expect(new Set(small.map(colorOf))).toEqual(new Set([ocean]))
    expect(new Set(large.map(colorOf))).toEqual(new Set([land, highlight]))
  })

  it('keeps painting every dot once while the map moves', () => {
    const canvas = recordingContext()
    const map = createMap(false)
    const still = canvas.frame(createMap(true)).length

    for (let frame = 0; frame < 3; frame++) {
      for (let step = 0; step < 40; step++) map.update()
      const dots = canvas.frame(map)

      expect(new Set(positions(dots)).size).toBe(dots.length)
      // Scrolling moves a column or two across the screen edges.
      expect(Math.abs(dots.length - still)).toBeLessThan(still * 0.02)
      expect(new Set(dots.map(colorOf))).toEqual(
        new Set([ocean, land, highlight])
      )
    }
  })

  it('tints the dots inside a ripple band with the ripple colour', () => {
    const canvas = recordingContext()
    const map = createMap(true)
    const ripple = { x: 700, y: 400, r: 60, color: '#ff0000', life: 0.5 }

    const dots = canvas.frame(map, [ripple])

    const tinted = dots.filter((dot) => colorOf(dot) === '255,0,0')
    const inBand = dots.filter((dot) => {
      const distance = Math.hypot(dot.x - ripple.x, dot.y - ripple.y)
      return distance >= ripple.r - 14 && distance <= ripple.r + 14
    })
    expect(tinted.length).toBeGreaterThan(0)
    expect(positions(tinted).sort()).toEqual(positions(inBand).sort())
    expect(dots.length).toBe(canvas.frame(map).length)
  })
})
