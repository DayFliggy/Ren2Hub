import { describe, expect, it, vi } from 'vitest'

import { Flyline } from '@/canvas/Flyline'

function recordingContext() {
  const calls: string[] = []
  const gradient = { addColorStop() {} }
  const ctx = new Proxy(
    {},
    {
      get: (_target, key) => {
        if (key === 'createRadialGradient') return () => gradient
        return () => calls.push(String(key))
      },
      set: () => true,
    }
  )
  return { ctx: ctx as CanvasRenderingContext2D, calls }
}

describe('Flyline', () => {
  // A dragged map wraps the off-screen marker across the seam; drawing its
  // flyline then would show the packet head jumping across the canvas.
  it('draws only while its model marker is on screen and still arrives', () => {
    const marker = {
      x: 300,
      y: 100,
      color: '#22c55e',
      visible: true,
      edgeAlpha: 1,
    }
    const onArrive = vi.fn()
    const flyline = new Flyline(
      { x: 0, y: 200 },
      marker,
      marker,
      1000,
      onArrive,
      () => ({ x: 200, y: 200 })
    )
    flyline.update(500)

    const shown = recordingContext()
    flyline.draw(shown.ctx, 0)
    expect(shown.calls).toContain('stroke')

    marker.visible = false
    const hidden = recordingContext()
    flyline.draw(hidden.ctx, 0)
    expect(hidden.calls).toEqual([])

    flyline.update(500)
    expect(onArrive).toHaveBeenCalledTimes(1)
  })
})
