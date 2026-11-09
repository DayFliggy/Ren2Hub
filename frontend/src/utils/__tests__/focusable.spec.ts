import { afterEach, describe, expect, it } from 'vitest'

import { tabbableElements } from '../focusable'

// jsdom has no layout, so offsetParent is null for every element; the filter
// keeps only the active element there. Give visible elements a parent.
function markVisible(root: HTMLElement) {
  for (const el of root.querySelectorAll<HTMLElement>('[data-visible]'))
    Object.defineProperty(el, 'offsetParent', { value: root })
}

describe('tabbableElements', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('skips roving-tabindex members that are not Tab stops', () => {
    document.body.innerHTML = `
      <div id="root">
        <button data-visible tabindex="-1">inactive card</button>
        <button data-visible tabindex="0">active card</button>
        <input data-visible tabindex="-1" />
        <button data-visible>save</button>
      </div>`
    const root = document.getElementById('root')!
    markVisible(root)

    expect(tabbableElements(root).map((el) => el.textContent)).toEqual([
      'active card',
      'save',
    ])
  })
})
