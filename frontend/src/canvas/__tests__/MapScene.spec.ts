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
})
