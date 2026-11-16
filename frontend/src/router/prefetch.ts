import {
  loadRouteLocation,
  type RouteLocationRaw,
  type Router,
} from 'vue-router'

import { loadMessageDomain } from '@/i18n'

// Route records whose code was already requested.
const started = new WeakSet<object>()

/**
 * Downloads a route's lazy components and messages ahead of the navigation,
 * so a click finds the page code already loaded. A failed download is not
 * reported here: the navigation requests the same files and reports it.
 */
export function prefetchRoute(router: Router, target: RouteLocationRaw): void {
  const route = router.resolve(target)
  const page = route.matched.at(-1)
  if (!page || started.has(page)) return
  started.add(page)
  const downloads: Promise<unknown>[] = [loadRouteLocation(route)]
  if (route.meta.messageDomain)
    downloads.push(loadMessageDomain(route.meta.messageDomain))
  void Promise.all(downloads).catch(() => started.delete(page))
}

// Long enough for an arriving page to render and finish its fade.
const ARRIVAL_MS = 400

/**
 * Prefetches the page the current one almost always leads to. It waits until
 * the current page has arrived, so parsing the next page's code does not
 * stall the arriving page.
 */
export function prefetchRouteAfterArrival(
  router: Router,
  target: RouteLocationRaw
): void {
  setTimeout(() => prefetchRoute(router, target), ARRIVAL_MS)
}

function prefetchTarget(
  router: Router,
  entry: Element
): RouteLocationRaw | null {
  const named = entry.getAttribute('data-prefetch-route')
  if (named !== null) return router.hasRoute(named) ? { name: named } : null

  const link = entry as HTMLAnchorElement
  if (
    link.origin !== window.location.origin ||
    (link.target && link.target !== '_self') ||
    link.hasAttribute('download')
  )
    return null
  const base = router.options.history.base
  const path = link.pathname.startsWith(base)
    ? link.pathname.slice(base.length)
    : link.pathname
  return (path || '/') + link.search
}

// A mouse crossing a menu passes over many entries; only one it rests on counts.
const HOVER_DWELL_MS = 80

/**
 * Prefetches a page when a mouse rests on a link to it, or on a control that
 * names its route in `data-prefetch-route`, and at once when a touch or the
 * keyboard reaches one. This is only a guess at the next page, so nothing is
 * fetched when the browser asks to save data. Returns a function that removes
 * the listeners.
 */
export function installRoutePrefetch(router: Router): () => void {
  const connection = (
    navigator as Navigator & { connection?: { saveData?: boolean } }
  ).connection
  if (connection?.saveData) return () => {}

  let dwell: ReturnType<typeof setTimeout> | undefined

  function targetOf(event: Event): RouteLocationRaw | null {
    if (!(event.target instanceof Element)) return null
    const entry = event.target.closest('a[href], [data-prefetch-route]')
    return entry && prefetchTarget(router, entry)
  }

  function onPointerOver(event: Event) {
    clearTimeout(dwell)
    const target = targetOf(event)
    if (!target) return
    // A touch fires pointerover just before pointerdown: it is already a tap.
    if ((event as PointerEvent).pointerType === 'touch')
      prefetchRoute(router, target)
    else dwell = setTimeout(() => prefetchRoute(router, target), HOVER_DWELL_MS)
  }

  function onFocusIn(event: Event) {
    const target = targetOf(event)
    if (target) prefetchRoute(router, target)
  }

  document.addEventListener('pointerover', onPointerOver, { passive: true })
  document.addEventListener('focusin', onFocusIn)
  return () => {
    clearTimeout(dwell)
    document.removeEventListener('pointerover', onPointerOver)
    document.removeEventListener('focusin', onFocusIn)
  }
}
