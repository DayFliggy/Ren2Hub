import { describe, expect, it } from 'vitest'

// Vue's scoped CSS keeps only the :global() part of a selector, so
// `:global(html.dark) .card` compiles to a bare `html.dark {}` that styles the
// root element and never reaches .card. A theme ancestor needs no :global at
// all: `html.dark .card` scopes only its last compound selector.
const sources = import.meta.glob<string>('@/**/*.vue', {
  query: '?raw',
  import: 'default',
  eager: true,
})

const SCOPED_STYLE = /<style\b[^>]*\bscoped\b[^>]*>([\s\S]*?)<\/style>/g
const TRUNCATED_GLOBAL = /:global\([^)]*\)\s*[^\s{,)]/g

describe('scoped style selectors', () => {
  it('never follow :global() with a selector Vue would drop', () => {
    const offenders = Object.entries(sources).flatMap(([file, source]) =>
      [...source.matchAll(SCOPED_STYLE)].flatMap(([, css]) =>
        [
          ...css!.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(TRUNCATED_GLOBAL),
        ].map(([selector]) => `${file}: ${selector}`)
      )
    )

    expect(Object.keys(sources).length).toBeGreaterThan(100)
    expect(offenders).toEqual([])
  })
})
