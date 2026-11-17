import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { MapScene } from '@/canvas/MapScene'
import { canStartCanvasRequest } from '@/canvas/requestGate'

vi.mock('@/canvas/iconLoader', () => ({
  loadIcon: async () => new Image(),
}))

describe('canvas request lifecycle', () => {
  it('enforces active-request and pointer cooldown limits', () => {
    expect(
      canStartCanvasRequest({
        activeRequests: 4,
        maxActiveRequests: 4,
        origin: 'auto',
        nowMs: 1_000,
        lastPointerRequestAt: 0,
        pointerCooldownMs: 450,
      })
    ).toBe(false)
    expect(
      canStartCanvasRequest({
        activeRequests: 1,
        maxActiveRequests: 4,
        origin: 'pointer',
        nowMs: 1_200,
        lastPointerRequestAt: 1_000,
        pointerCooldownMs: 450,
      })
    ).toBe(false)
  })

  it('terminates group artifacts and clears retained arrays on dispose', () => {
    const channel = { dead: false }
    const flyline = { dead: false }
    const packet = { dead: false }
    const scene = Object.create(MapScene.prototype) as Record<
      string,
      unknown
    > & {
      dispose: () => void
    }
    Object.assign(scene, {
      disposed: false,
      edgeBoosting: false,
      running: false,
      raf: 0,
      groups: [
        {
          channels: [channel],
          flylines: [flyline],
          packets: [packet],
          processingDueAt: new Map([['model', 1]]),
        },
      ],
      users: [{}],
      channels: [channel],
      flylines: [flyline],
      packets: [packet],
      particles: [{}],
      ripples: [{}],
      completedTraces: [{}],
      latencySamples: [10],
      resetEdgeBoost: vi.fn(),
      stop: vi.fn(),
    })

    scene.dispose()

    expect(channel.dead).toBe(true)
    expect(flyline.dead).toBe(true)
    expect(packet.dead).toBe(true)
    expect(scene.groups).toEqual([])
    expect(scene.particles).toEqual([])
  })
})

interface ScenePoint {
  x: number
  y: number
}

interface SceneInternals {
  markers: ScenePoint[]
  groups: Array<{
    phase: string
    markers: ScenePoint[]
    flylines: Array<{
      dead: boolean
      point: (t: number) => ScenePoint
    }>
  }>
  sceneTimeMs: number
  draw: () => void
  rain: { drops: number[] }
  map: { stillFrames: number }
}

describe('canvas scene motion', () => {
  let now = 0
  let frames: Map<number, FrameRequestCallback>
  let frameId = 0
  let reduceQuery: EventTarget & { matches: boolean; media: string }
  let rect: DOMRect

  function setReducedMotion(matches: boolean) {
    reduceQuery.matches = matches
    reduceQuery.dispatchEvent(Object.assign(new Event('change'), { matches }))
  }

  function runFrame() {
    now += 100
    const pending = [...frames.values()]
    frames.clear()
    pending.forEach((callback) => callback(now))
  }

  async function createScene() {
    const canvas = document.createElement('canvas')
    const gradient = { addColorStop() {} }
    const context = new Proxy(
      {},
      {
        get: (_target, key) => {
          if (key === 'measureText') return () => ({ width: 30 })
          if (key === 'createLinearGradient' || key === 'createRadialGradient')
            return () => gradient
          return () => {}
        },
        set: () => true,
      }
    )
    vi.spyOn(canvas, 'getContext').mockReturnValue(
      context as CanvasRenderingContext2D
    )
    vi.spyOn(canvas, 'getBoundingClientRect').mockImplementation(() => rect)
    const scene = new MapScene(canvas)
    await scene.init()
    return scene
  }

  beforeEach(() => {
    now = 0
    frames = new Map()
    rect = DOMRect.fromRect({ x: 0, y: 0, width: 390, height: 844 })
    reduceQuery = Object.assign(new EventTarget(), {
      matches: false,
      media: '(prefers-reduced-motion: reduce)',
    })
    vi.spyOn(performance, 'now').mockImplementation(() => now)
    vi.spyOn(Math, 'random').mockReturnValue(0.5)
    vi.stubGlobal('matchMedia', () => reduceQuery)
    vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => {
      frames.set(++frameId, callback)
      return frameId
    })
    vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })

  // start() under reduced motion still opens the visibility gate, so turning
  // the preference off later must start the frame loop without a reload.
  it('resumes the frame loop when reduced motion is turned off after load', async () => {
    reduceQuery.matches = true
    const scene = await createScene()
    scene.start()
    expect(frames.size).toBe(0)

    setReducedMotion(false)
    expect(frames.size).toBe(1)

    setReducedMotion(true)
    expect(frames.size).toBe(0)
    scene.stop()
    setReducedMotion(false)
    expect(frames.size).toBe(0)
    scene.dispose()
  })

  /**
   * Runs a scene on a display of the given refresh rate, with every frame
   * taking `cost()` milliseconds to produce.
   */
  function onDisplay(scene: MapScene, hz: number, cost: () => number) {
    const internals = scene as unknown as SceneInternals
    const draw = internals.draw.bind(scene)
    let drawn = 0
    internals.draw = () => {
      draw()
      drawn++
      now += cost()
    }
    let display = now
    return {
      /** One second of display frames: the frames drawn and scene time passed. */
      second() {
        const before = { drawn, time: internals.sceneTimeMs }
        for (let frame = 0; frame < hz; frame++) {
          display += 1000 / hz
          now = Math.max(now, display)
          const pending = [...frames.values()]
          frames.clear()
          pending.forEach((callback) => callback(now))
        }
        return {
          drawn: drawn - before.drawn,
          time: Math.round(internals.sceneTimeMs - before.time),
        }
      },
    }
  }

  // A device that spends much of a display frame producing one frame of the
  // scene leaves every other display frame to the rest of the page. A display
  // that is already that slow keeps its rate.
  it.each([
    { hz: 60, slow: 30, fast: 60 },
    { hz: 120, slow: 30, fast: 60 },
    { hz: 30, slow: 30, fast: 30 },
  ])(
    'draws $slow frames a second on a $hz Hz display while frames are slow to produce',
    async ({ hz, slow, fast }) => {
      const scene = await createScene()
      let drawMs = 12
      const display = onDisplay(scene, hz, () => drawMs)
      scene.start()

      display.second()
      expect(display.second()).toEqual({ drawn: slow, time: 1000 })

      drawMs = 0.5
      display.second()
      expect(display.second()).toEqual({ drawn: fast, time: 1000 })
      scene.dispose()
    }
  )

  // Motion follows the clock, not the number of frames drawn: the character
  // rain falls and a resting pointer raises its ripples as fast as before.
  it('keeps ambient motion at its speed while frames are drawn half as often', async () => {
    async function twoSeconds(drawMs: number) {
      const scene = await createScene()
      const internals = scene as unknown as SceneInternals
      const display = onDisplay(scene, 60, () => drawMs)
      scene.setMouse(200, 300, true)
      scene.start()
      const rainStart = internals.rain.drops[1]
      display.second()
      display.second()
      const motion = {
        rainFall: internals.rain.drops[1] - rainStart,
        pointerRest: internals.map.stillFrames,
      }
      scene.dispose()
      return motion
    }

    const smooth = await twoSeconds(0.5)
    const spaced = await twoSeconds(12)

    expect(smooth.pointerRest).toBeGreaterThan(110)
    expect(Math.abs(spaced.pointerRest - smooth.pointerRest)).toBeLessThan(3)
    expect(Math.abs(spaced.rainFall / smooth.rainFall - 1)).toBeLessThan(0.03)
  })

  // Relayouts (resize, immersive toggle) move the model markers; an in-flight
  // packet must keep heading for the marker, not its pre-layout position.
  it('keeps an outbound flyline attached to its marker across a relayout', async () => {
    const scene = await createScene()
    const internals = scene as unknown as SceneInternals
    scene.start()
    let route: SceneInternals['groups'][number] | undefined
    for (let i = 0; i < 200 && !route; i++) {
      runFrame()
      route = internals.groups.find(
        (group) =>
          group.phase === 'route' &&
          group.flylines.length > 0 &&
          !group.flylines[0].dead
      )
    }
    expect(route).toBeDefined()
    const flyline = route!.flylines[0]
    const marker = route!.markers[0]
    const before = { x: marker.x, y: marker.y }

    rect = DOMRect.fromRect({ x: 0, y: 0, width: 1280, height: 720 })
    scene.resize()

    expect(
      Math.hypot(marker.x - before.x, marker.y - before.y)
    ).toBeGreaterThan(1)
    expect(flyline.point(1).x).toBeCloseTo(marker.x, 6)
    expect(flyline.point(1).y).toBeCloseTo(marker.y, 6)
    scene.dispose()
  })

  it('keeps model markers below the navigation bar', async () => {
    rect = DOMRect.fromRect({ x: 0, y: 0, width: 360, height: 640 })
    const scene = await createScene()
    const markers = (
      scene as unknown as {
        markers: { y: number; r: number; visible: boolean }[]
      }
    ).markers
    const reaching = (inset: number) =>
      markers.filter((marker) => marker.visible && marker.y - marker.r < inset)

    scene.resize(null, null, 0)
    const inset = Math.max(...markers.map((marker) => marker.y - marker.r)) / 2
    expect(reaching(inset).length).toBeGreaterThan(0)

    scene.resize(null, null, inset)
    expect(reaching(inset + 6)).toEqual([])
    scene.dispose()
  })

  it('draws a phone stage at 1.5 device pixels per CSS pixel at most', async () => {
    vi.stubGlobal('devicePixelRatio', 3)
    const scene = await createScene()
    const canvas = (scene as unknown as { canvas: HTMLCanvasElement }).canvas

    expect(canvas.width).toBe(Math.floor(390 * 1.5))
    rect = DOMRect.fromRect({ x: 0, y: 0, width: 1440, height: 900 })
    scene.resize()
    expect(canvas.width).toBe(1440 * 2)
    scene.dispose()
  })
})
