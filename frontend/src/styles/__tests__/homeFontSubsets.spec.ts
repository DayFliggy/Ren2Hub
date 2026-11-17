import { describe, expect, it } from 'vitest'

import faces from '../../../scripts/home-font-faces.json'

type Message = string | Message[] | { [key: string]: Message }

const stylesheets = import.meta.glob<string>('../home-font-subsets.css', {
  eager: true,
  query: '?raw',
  import: 'default',
})
const locales = import.meta.glob<Message>('../../i18n/locales/*/home.ts', {
  eager: true,
  import: 'default',
})
// Each subset file as a base64 data URL.
const fontFiles = import.meta.glob<string>(
  '../../assets/fonts/home/subset/*.woff2',
  { eager: true, query: '?inline', import: 'default' }
)
const stylesheet = Object.values(stylesheets)[0] ?? ''

function strings(message: Message): string[] {
  if (typeof message === 'string') return [message]
  return Object.values(message).flatMap(strings)
}

function copyOf(messages: Message, path: string): string {
  let message: Message | undefined = messages
  for (const key of path.split('.'))
    message = (message as Record<string, Message> | undefined)?.[key]
  if (message === undefined) throw new Error(`no home message "${path}"`)
  return strings(message).join('')
}

/** The code points a family's subset files are declared for. */
function subsetCodePoints(family: string): Set<number> {
  const points = new Set<number>()
  for (const rule of stylesheet.split('@font-face').slice(1)) {
    if (!rule.includes(`'${family}'`) || !rule.includes('/home/subset/'))
      continue
    const range = /unicode-range:([^;]+);/.exec(rule)?.[1] ?? ''
    for (const term of range.matchAll(/U\+([0-9A-F]+)(?:-([0-9A-F]+))?/g)) {
      const from = parseInt(term[1], 16)
      const to = parseInt(term[2] ?? term[1], 16)
      for (let point = from; point <= to; point++) points.add(point)
    }
  }
  return points
}

// The subset files are committed, cut from the copy by `bun run fonts:home`.
// Copy that changes without them makes every visit download a complete
// display font for the characters the subset lacks.
describe('home font subsets', () => {
  it.each(faces)('$family covers its copy in every language', (face) => {
    const subset = subsetCodePoints(face.family)
    const copy = Object.values(locales).flatMap((messages) =>
      face.copy.map((path) => copyOf(messages, path))
    )

    const missing = [...new Set(copy.join(''))].filter(
      (character) => !subset.has(character.codePointAt(0)!)
    )

    expect(missing.join('')).toBe('')
  })

  // A rule and its font file are generated together. A file from another run
  // would lack glyphs its rule declares, and that text would silently render
  // in another typeface.
  it('ships each subset file its rule was generated with', async () => {
    const shipped: Record<string, string> = {}
    for (const [path, dataUrl] of Object.entries(fontFiles)) {
      const bytes = Uint8Array.from(atob(dataUrl.split(',')[1]), (byte) =>
        byte.charCodeAt(0)
      )
      const digest = await crypto.subtle.digest('SHA-256', bytes)
      shipped[path.split('/').at(-1)!] = [...new Uint8Array(digest)]
        .map((byte) => byte.toString(16).padStart(2, '0'))
        .join('')
    }
    const declared = Object.fromEntries(
      [
        ...stylesheet.matchAll(/\/\* (\S+\.woff2) sha256 ([0-9a-f]{64}) \*\//g),
      ].map((rule) => [rule[1], rule[2]])
    )

    expect(Object.keys(declared).length).toBeGreaterThan(0)
    expect(shipped).toEqual(declared)
  })
})
