import { afterEach, describe, expect, it } from 'vitest'
import { lockBodyScroll, unlockBodyScroll } from '../modalStack'

afterEach(() => {
  document.body.style.overflow = ''
  document.documentElement.style.overflow = ''
})

describe('shared overlay scroll lock', () => {
  it('locks the console scroll surface and restores it after the last overlay closes', () => {
    document.body.innerHTML =
      '<div id="app"><main data-console-scroll-zone style="overflow:auto"></main></div>'
    document.body.style.overflow = 'scroll'
    const app = document.getElementById('app')!
    const surface = document.querySelector<HTMLElement>('main')!
    app.inert = false
    const first = Symbol('drawer')
    const second = Symbol('dialog')
    lockBodyScroll(first)
    lockBodyScroll(second)
    expect(document.body.style.overflow).toBe('hidden')
    expect(surface.style.overflow).toBe('hidden')
    expect(app.inert).toBe(true)
    unlockBodyScroll(first)
    expect(surface.style.overflow).toBe('hidden')
    expect(app.inert).toBe(true)
    unlockBodyScroll(second)
    expect(document.body.style.overflow).toBe('scroll')
    expect(surface.style.overflow).toBe('auto')
    expect(app.inert).toBe(false)
  })

  it('keeps the public page locked while a dialog outlives the navigation drawer', () => {
    document.body.innerHTML = '<div id="app"></div>'
    const root = document.documentElement
    root.style.overflow = 'clip'
    const drawer = Symbol('AppNavbarDrawer')
    const dialog = Symbol('SecurityProofDialog')

    lockBodyScroll(drawer)
    lockBodyScroll(dialog)
    // A repeated lock or a stray release must not touch the shared count.
    lockBodyScroll(drawer)
    unlockBodyScroll(Symbol('never locked'))
    unlockBodyScroll(drawer)
    unlockBodyScroll(drawer)
    expect(root.style.overflow).toBe('hidden')
    expect(document.body.style.overflow).toBe('hidden')

    unlockBodyScroll(dialog)
    expect(root.style.overflow).toBe('clip')
    expect(document.body.style.overflow).toBe('')
  })
})
