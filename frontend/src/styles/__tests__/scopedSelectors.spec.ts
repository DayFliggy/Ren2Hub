import { compileStyle, parse } from '@vue/compiler-sfc'
import { describe, expect, it } from 'vitest'

// Vue's scoped CSS keeps only the :global() part of a selector when :global()
// is used as an ancestor prefix, so `:global(html.dark) .card` compiles to a
// bare `html.dark {}` that styles the root element and never reaches `.card`.
const sources = import.meta.glob<string>('@/**/*.vue', {
  query: '?raw',
  import: 'default',
  eager: true,
})

/**
 * Finds `:global(...)` calls with balanced parentheses and reports any selector
 * that continues with trailing tokens before `,` or `{` (which Vue's scoped
 * compiler would silently drop).
 */
export function findTruncatedGlobalSelectors(css: string): string[] {
  const stripped = css.replace(/\/\*[\s\S]*?\*\//g, '')
  const offenders: string[] = []
  const marker = ':global('
  let searchFrom = 0

  while (searchFrom < stripped.length) {
    const start = stripped.indexOf(marker, searchFrom)
    if (start === -1) break

    let depth = 1
    let cursor = start + marker.length
    let inSingleQuote = false
    let inDoubleQuote = false

    while (cursor < stripped.length && depth > 0) {
      const ch = stripped[cursor]
      const prev = stripped[cursor - 1]
      if (ch === "'" && !inDoubleQuote && prev !== '\\') {
        inSingleQuote = !inSingleQuote
      } else if (ch === '"' && !inSingleQuote && prev !== '\\') {
        inDoubleQuote = !inDoubleQuote
      } else if (!inSingleQuote && !inDoubleQuote) {
        if (ch === '(') depth++
        else if (ch === ')') depth--
      }
      cursor++
    }

    if (depth === 0) {
      // Inspect what follows the balanced closing `)` of `:global(...)` up to
      // the next selector separator `,` or rule block `{`.
      let tailEnd = cursor
      while (
        tailEnd < stripped.length &&
        stripped[tailEnd] !== ',' &&
        stripped[tailEnd] !== '{' &&
        stripped[tailEnd] !== ')'
      ) {
        tailEnd++
      }
      const tail = stripped.slice(cursor, tailEnd).trim()
      if (tail.length > 0) {
        offenders.push(stripped.slice(start, tailEnd).trim())
      }
    }

    searchFrom = cursor
  }

  return offenders
}

const BARE_ROOT_RULE =
  /(?:^|\}|@media[^{]+\{)\s*(?:html(?:\.(?:dark|light))?(?:\[[^\]]+\])*|:root)\s*\{/

describe('scoped style selectors', () => {
  it('accurately distinguishes valid nested pseudo-classes inside :global() from truncated trailing selectors', () => {
    expect(
      findTruncatedGlobalSelectors(
        ":global(html.dark .console-sidebar__item:not([aria-current='page']):not(:disabled):hover) { color: red; }"
      )
    ).toEqual([])

    expect(
      findTruncatedGlobalSelectors(
        ':global(html.dark) .console-sidebar__active-bar { display: none; }'
      )
    ).toEqual([':global(html.dark) .console-sidebar__active-bar'])
  })

  it('never follows :global() with a selector Vue would drop or compiles to a bare html/root rule', () => {
    const offenders: string[] = []

    for (const [file, source] of Object.entries(sources)) {
      const { descriptor, errors } = parse(source, { filename: file })
      if (errors.length > 0) {
        offenders.push(`${file}: parse error ${errors[0]!.message}`)
        continue
      }

      for (const style of descriptor.styles) {
        if (!style.scoped) continue

        for (const truncated of findTruncatedGlobalSelectors(style.content)) {
          offenders.push(`${file}: truncated :global selector "${truncated}"`)
        }

        const compiled = compileStyle({
          source: style.content,
          filename: file,
          id: 'data-v-scoped-check',
          scoped: true,
        })

        if (compiled.errors.length > 0) {
          offenders.push(
            `${file}: compile error ${compiled.errors[0]!.message}`
          )
          continue
        }

        const cleanCode = compiled.code.replace(/\/\*[\s\S]*?\*\//g, '')
        if (BARE_ROOT_RULE.test(cleanCode)) {
          offenders.push(`${file}: compiled bare root rule in scoped style`)
        }
      }
    }

    expect(Object.keys(sources).length).toBeGreaterThan(100)
    expect(offenders).toEqual([])
  })
})
