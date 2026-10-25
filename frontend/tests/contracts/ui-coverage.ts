import { existsSync, readFileSync, readdirSync, writeFileSync } from 'node:fs'
import { createHash } from 'node:crypto'
import { basename, dirname } from 'node:path'
import {
  expect,
  type ElementHandle,
  type Page,
  type TestInfo,
} from '@playwright/test'

type Position = { left: number; top: number }
type Registry = {
  next: number
  ids: WeakMap<Element, number>
  elements: Map<number, Element>
  original: WeakMap<Element, Position>
}
type Scroller = Position & {
  id: number
  parents: number[]
  width: number
  height: number
  maxX: number
  maxY: number
  x: boolean
  y: boolean
}

/** Runs in the browser. A component counts only where some painted box survives
 * viewport, ancestor overflow, opacity and screen-reader clipping. */
function inspectDOM(registry: Registry) {
  type Instance = { type?: { __file?: string }; parent?: Instance }
  type Rect = { left: number; top: number; right: number; bottom: number }
  const mounted = new Set<string>()
  const visible = new Set<string>()
  const scrollers: Scroller[] = []
  const styles = new Map<Element, CSSStyleDeclaration>()
  const bounds = new Map<Element, DOMRect>()
  const styleFor = (element: Element) => {
    if (!styles.has(element)) styles.set(element, getComputedStyle(element))
    return styles.get(element)!
  }
  const boundsFor = (element: Element) => {
    if (!bounds.has(element))
      bounds.set(element, element.getBoundingClientRect())
    return bounds.get(element)!
  }
  const clip = (rect: Rect, bounds: Rect): Rect => ({
    left: Math.max(rect.left, bounds.left),
    top: Math.max(rect.top, bounds.top),
    right: Math.min(rect.right, bounds.right),
    bottom: Math.min(rect.bottom, bounds.bottom),
  })
  const area = (rect: Rect) => rect.right > rect.left && rect.bottom > rect.top
  function shown(element: Element) {
    if (['hidden', 'collapse'].includes(styleFor(element).visibility))
      return false
    let boxes = [...element.getClientRects()]
      .map((rect) =>
        clip(rect, { left: 0, top: 0, right: innerWidth, bottom: innerHeight })
      )
      .filter(area)
    for (
      let node: Element | null = element;
      node && boxes.length;
      node = node.parentElement
    ) {
      const style = styleFor(node)
      if (
        style.display === 'none' ||
        Number(style.opacity) === 0 ||
        style.contentVisibility === 'hidden'
      )
        return false
      const rect = boundsFor(node)
      // Tailwind sr-only uses legacy rect clipping; inset(50%) is the modern
      // equivalent. Neither creates visible pixels despite a nonzero DOM rect.
      const legacy = /^rect\(([^)]+)\)$/.exec(style.clip)
      if (legacy) {
        const edges = legacy[1]!
          .split(/[ ,]+/)
          .map((value, index) =>
            value === 'auto'
              ? [0, rect.width, rect.height, 0][index]!
              : Number.parseFloat(value)
          )
        boxes = boxes
          .map((box) =>
            clip(box, {
              top: rect.top + edges[0]!,
              right: rect.left + edges[1]!,
              bottom: rect.top + edges[2]!,
              left: rect.left + edges[3]!,
            })
          )
          .filter(area)
      }
      const inset = /^inset\(([^)]+)\)$/.exec(style.clipPath)
      if (inset) {
        const values = inset[1]!
          .split(/\s+round\s+/)[0]!
          .trim()
          .split(/\s+/)
        const edges = [
          values[0]!,
          values[1] ?? values[0]!,
          values[2] ?? values[0]!,
          values[3] ?? values[1] ?? values[0]!,
        ]
        const amount = (value: string, dimension: number) =>
          Number.parseFloat(value) * (value.endsWith('%') ? dimension / 100 : 1)
        boxes = boxes
          .map((box) =>
            clip(box, {
              top: rect.top + amount(edges[0]!, rect.height),
              right: rect.right - amount(edges[1]!, rect.width),
              bottom: rect.bottom - amount(edges[2]!, rect.height),
              left: rect.left + amount(edges[3]!, rect.width),
            })
          )
          .filter(area)
      }
      // The viewport already clips document roots; fixed-body modal locks may
      // give those layout boxes zero height while their children remain visible.
      if (
        node === element ||
        node === document.body ||
        node === document.documentElement
      )
        continue
      const x = /auto|scroll|hidden|clip/.test(style.overflowX)
      const y = /auto|scroll|hidden|clip/.test(style.overflowY)
      if (x || y) {
        const html = node as HTMLElement
        const scaleX = html.offsetWidth ? rect.width / html.offsetWidth : 1
        const scaleY = html.offsetHeight ? rect.height / html.offsetHeight : 1
        const left = rect.left + node.clientLeft * scaleX
        const top = rect.top + node.clientTop * scaleY
        boxes = boxes
          .map((box) =>
            clip(box, {
              left: x ? left : -Infinity,
              right: x ? left + node.clientWidth * scaleX : Infinity,
              top: y ? top : -Infinity,
              bottom: y ? top + node.clientHeight * scaleY : Infinity,
            })
          )
          .filter(area)
      }
    }
    return boxes.length > 0
  }
  for (const element of document.querySelectorAll('*')) {
    if (!registry.original.has(element))
      registry.original.set(element, {
        left: element.scrollLeft,
        top: element.scrollTop,
      })
    const isShown = shown(element)
    let instance = (element as Element & { __vueParentComponent?: Instance })
      .__vueParentComponent
    const visited = new Set<Instance>()
    while (instance && !visited.has(instance)) {
      visited.add(instance)
      const file = instance.type?.__file
        ?.match(/(?:^|\/)src\/.*\.vue$/)?.[0]
        .replace(/^\//, '')
      if (file) {
        mounted.add(file)
        if (isShown) visible.add(file)
      }
      instance = instance.parent
    }
    const style = styleFor(element)
    const root = element === document.scrollingElement
    const x =
      element.scrollWidth > element.clientWidth + 1 &&
      ((root && style.overflowX === 'visible') ||
        /auto|scroll/.test(style.overflowX))
    const y =
      element.scrollHeight > element.clientHeight + 1 &&
      ((root && style.overflowY === 'visible') ||
        /auto|scroll/.test(style.overflowY))
    if (
      !isShown ||
      element.clientWidth < 2 ||
      element.clientHeight < 2 ||
      (!x && !y)
    )
      continue
    let id = registry.ids.get(element)
    if (id === undefined) {
      id = registry.next++
      registry.ids.set(element, id)
      registry.elements.set(id, element)
    }
    scrollers.push({
      id,
      parents: [],
      left: element.scrollLeft,
      top: element.scrollTop,
      width: element.clientWidth,
      height: element.clientHeight,
      maxX: element.scrollWidth - element.clientWidth,
      maxY: element.scrollHeight - element.clientHeight,
      x,
      y,
    })
  }
  for (const scroller of scrollers) {
    for (
      let node = registry.elements.get(scroller.id)!.parentElement;
      node;
      node = node.parentElement
    ) {
      const id = registry.ids.get(node)
      if (id !== undefined) scroller.parents.push(id)
    }
  }
  return {
    mounted: [...mounted].sort(),
    visible: [...visible].sort(),
    scrollers,
  }
}

function stops(
  enabled: boolean,
  maximum: number,
  viewport: number,
  current: number
) {
  if (!enabled) return [current]
  const points = [0]
  const step = Math.max(1, Math.floor(viewport * 0.85))
  while (points.at(-1)! < maximum)
    points.push(Math.min(points.at(-1)! + step, maximum))
  return points
}

/** Record rendered instances and all reachable scroll frames, without changing
 * application styles, disabling clipping or accepting new reference snapshots. */
export async function captureUI(
  page: Page,
  info: TestInfo,
  label: string,
  options: { scroll?: boolean; pausedClock?: boolean; compare?: boolean } = {}
) {
  await page.evaluate(() => document.fonts.ready)
  const registry = await page.evaluateHandle((): Registry => ({
    next: 0,
    ids: new WeakMap(),
    elements: new Map(),
    original: new WeakMap(),
  }))
  const handles = new Map<number, ElementHandle<Element>>()
  const read = () => page.evaluate(inspectDOM, registry)
  const components = await read()
  const screenshots: string[] = []
  const frames: Array<{ name: string; visible: string[] }> = []
  const identity = createHash('sha256')
    .update(info.titlePath.join(' / '))
    .digest('hex')
    .slice(0, 16)
  const settle = async () => {
    if (options.pausedClock) await page.clock.runFor(32)
    else
      await page.evaluate(
        () =>
          new Promise<void>((resolve) =>
            requestAnimationFrame(() => requestAnimationFrame(() => resolve()))
          )
      )
  }
  const handleFor = async (id: number) => {
    if (!handles.has(id)) {
      const handle = await registry.evaluateHandle(
        (state, key) => state.elements.get(key)!,
        id
      )
      handles.set(id, handle.asElement()!)
    }
    return handles.get(id)!
  }
  const move = async (element: ElementHandle<Element>, position: Position) => {
    await element.evaluate(
      (node, { target, pausedClock }) => {
        if (!node.isConnected) return
        const before = { left: node.scrollLeft, top: node.scrollTop }
        const events = node === document.scrollingElement ? document : node
        let complete: () => void = () => {}
        const scrolled = pausedClock
          ? new Promise<void>((resolve) => {
              complete = () => {
                events.removeEventListener('scroll', complete)
                resolve()
              }
              events.addEventListener('scroll', complete, { once: true })
            })
          : undefined
        node.scrollTo({
          left: target.left,
          top: target.top,
          behavior: 'instant',
        })
        // Native scroll events enqueue component RAF callbacks. Let the browser
        // dispatch that event before advancing a paused animation clock.
        if (node.scrollLeft === before.left && node.scrollTop === before.top)
          complete()
        return scrolled
      },
      { target: position, pausedClock: options.pausedClock === true }
    )
    await settle()
  }
  const frame = async (name: string, fullPage = false) => {
    screenshots.push(name)
    if (options.compare && process.env.REN2HUB_UI_BASELINE)
      await expect.soft(page).toHaveScreenshot(`${identity}-${name}`, {
        fullPage,
        animations: 'disabled',
        caret: 'hide',
        maxDiffPixels: 0,
      })
    await page.screenshot({
      path: info.outputPath(name),
      fullPage,
      animations: 'disabled',
      caret: 'hide',
    })
    const current = await read()
    components.mounted = [
      ...new Set([...components.mounted, ...current.mounted]),
    ].sort()
    components.visible = [
      ...new Set([...components.visible, ...current.visible]),
    ].sort()
    frames.push({ name, visible: current.visible })
  }
  const walk = async (
    scroller: Scroller,
    ancestors: Set<number>,
    path: string
  ) => {
    const element = await handleFor(scroller.id)
    const original = await element.evaluate((node) => ({
      left: node.scrollLeft,
      top: node.scrollTop,
    }))
    try {
      for (const top of stops(
        scroller.y,
        scroller.maxY,
        scroller.height,
        scroller.top
      )) {
        for (const left of stops(
          scroller.x,
          scroller.maxX,
          scroller.width,
          scroller.left
        )) {
          await move(element, { top, left })
          const position = await element.evaluate((node) => ({
            left: node.scrollLeft,
            top: node.scrollTop,
          }))
          const step = `${path}${scroller.id}-x${Math.round(position.left)}-y${Math.round(position.top)}`
          await frame(`${label}-scroll-${step}.png`)
          // DataTable activates its inner scrolling only after its parent has
          // reached the header. Discover children here, keeping the parent put.
          const current = await read()
          const parents = new Set([...ancestors, scroller.id])
          for (const child of current.scrollers.filter(
            (item) =>
              item.parents.includes(scroller.id) &&
              !parents.has(item.id) &&
              item.parents.every((id) => parents.has(id))
          ))
            await walk(child, parents, `${step}-`)
        }
      }
    } finally {
      await move(element, original)
    }
  }
  try {
    await frame(`${label}.png`, true)
    const floatingOpen =
      (await page
        .locator('[role="menu"]:visible, [role="listbox"]:visible')
        .count()) > 0
    if (options.scroll !== false && !floatingOpen) {
      for (const scroller of (await read()).scrollers.filter(
        (item) => item.parents.length === 0
      ))
        await walk(scroller, new Set(), '')
    }
  } finally {
    // A parent's scroll handoff can reset an inner table. Restore the initial
    // positions parent-first after traversal, including those indirect resets.
    const originals = await registry.evaluate((state) =>
      [...state.elements]
        .map(([id, element]) => {
          let depth = 0
          for (
            let parent = element.parentElement;
            parent;
            parent = parent.parentElement
          )
            depth++
          return { id, depth, ...state.original.get(element)! }
        })
        .sort((a, b) => a.depth - b.depth)
    )
    for (const position of originals)
      await move(await handleFor(position.id), position)
    for (const element of handles.values()) await element.dispose()
    await registry.dispose()
  }
  if (options.compare && process.env.REN2HUB_UI_BASELINE) {
    const initial = info.snapshotPath(`${identity}-${label}.png`)
    const directory = dirname(initial)
    const prefix = `${identity}-${label}-scroll-`
    const actual = existsSync(directory)
      ? readdirSync(directory)
          .filter(
            (name) =>
              name === basename(initial) ||
              (name.startsWith(prefix) && name.endsWith('.png'))
          )
          .sort()
      : []
    const expected = screenshots
      .map((name) => basename(info.snapshotPath(`${identity}-${name}`)))
      .sort()
    expect
      .soft(
        actual,
        'The reference must contain exactly this checkpoint’s complete frame set'
      )
      .toEqual(expected)
  }
  const preferences = await page.evaluate(() => ({
    theme: localStorage.getItem('ren2hub_theme_mode'),
    locale: localStorage.getItem('ren2hub_locale'),
  }))
  const sourceHashes = Object.fromEntries(
    components.mounted.map((file) => [
      file,
      createHash('sha256').update(readFileSync(file)).digest('hex'),
    ])
  )
  writeFileSync(
    info.outputPath(`${label}.coverage.json`),
    JSON.stringify(
      {
        testId: info.testId,
        title: info.title,
        label,
        snapshotIdentity: identity,
        url: page.url(),
        viewport: page.viewportSize(),
        ...preferences,
        sourceHashes,
        screenshots,
        frames,
        mounted: components.mounted,
        visible: components.visible,
      },
      null,
      2
    )
  )
  expect(
    components.visible.length,
    'The rendered page must expose Vue components'
  ).toBeGreaterThan(0)
}
