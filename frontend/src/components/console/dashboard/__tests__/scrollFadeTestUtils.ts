import { vi } from 'vitest'

/**
 * jsdom has no layout, so scroll-fade specs drive the box geometry by hand and
 * fire ResizeObserver callbacks themselves.
 */

export interface CapturedResizeObserver {
  targets: Set<Element>
  fire: () => void
}

export const resizeObservers: CapturedResizeObserver[] = []

class CapturingResizeObserver {
  targets = new Set<Element>()

  constructor(private readonly callback: ResizeObserverCallback) {
    resizeObservers.push(this)
  }

  observe(target: Element) {
    this.targets.add(target)
  }

  unobserve(target: Element) {
    this.targets.delete(target)
  }

  disconnect() {
    this.targets.clear()
  }

  fire() {
    this.callback([], this as unknown as ResizeObserver)
  }
}

export function captureResizeObservers() {
  resizeObservers.length = 0
  vi.stubGlobal('ResizeObserver', CapturingResizeObserver)
}

/** Overrides one element's box; own properties shadow any prototype spy. */
export function setScrollGeometry(
  el: HTMLElement,
  geometry: { clientWidth: number; scrollWidth: number; scrollLeft?: number }
) {
  Object.defineProperty(el, 'clientWidth', {
    configurable: true,
    value: geometry.clientWidth,
  })
  Object.defineProperty(el, 'scrollWidth', {
    configurable: true,
    value: geometry.scrollWidth,
  })
  Object.defineProperty(el, 'scrollLeft', {
    configurable: true,
    writable: true,
    value: geometry.scrollLeft ?? 0,
  })
}

/**
 * Gives every element matching `selector` an overflowing box, including ones
 * that do not exist yet — for tables that only render after loading.
 */
export function overflowScrollRegions(
  selector: string,
  clientWidth: number,
  scrollWidth: number
) {
  // jsdom defines both boxes on Element.prototype and reports 0 everywhere.
  vi.spyOn(Element.prototype, 'clientWidth', 'get').mockImplementation(
    function (this: Element) {
      return this.matches(selector) ? clientWidth : 0
    }
  )
  vi.spyOn(Element.prototype, 'scrollWidth', 'get').mockImplementation(
    function (this: Element) {
      return this.matches(selector) ? scrollWidth : 0
    }
  )
}
