/**
 * Builds the font files the home page loads: each display font cut down to the
 * copy it renders, in every language. Run `bun run fonts:home` after changing
 * that copy or `home-font-faces.json`.
 *
 * Each face in `home-font-faces.json` names the `home` messages it renders
 * (`copy`), the subset files to write and the font each is cut from (`files`).
 * `rest` is the complete font, declared for every other character, so text
 * outside a subset keeps its typeface; a face without it leaves such text to
 * the next family of the page's font stack. `axes` pins a variable font to the
 * one instance the page uses.
 *
 * Runs on Node 22.18 or later, which imports the TypeScript message files as
 * they are.
 */
import { createHash } from 'node:crypto'
import { mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises'
import { dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

import { Blob, Face } from 'harfbuzzjs'
import subsetFont from 'subset-font'

import FACES from './home-font-faces.json' with { type: 'json' }

const frontend = new URL('../', import.meta.url)
const FONT_DIR = 'src/assets/fonts/'
const SUBSET_DIR = `${FONT_DIR}home/subset/`
const STYLESHEET = 'src/styles/home-font-subsets.css'
const LOCALE_DIR = 'src/i18n/locales/'

// Counters, units and model names reach these elements from data, so every
// subset carries printable ASCII besides its copy.
const ASCII = Array.from({ length: 95 }, (_, index) => 0x20 + index)
const LAST_CODE_POINT = 0x10ffff

/** The `home` messages of every language. */
async function homeMessages() {
  const locales = await readdir(new URL(LOCALE_DIR, frontend))
  return Promise.all(
    locales.sort().map(async (locale) => {
      const file = new URL(`${LOCALE_DIR}${locale}/home.ts`, frontend)
      return (await import(file.href)).default
    })
  )
}

/** Every string under a message, which may be text, a list or a group. */
function strings(message) {
  if (typeof message === 'string') return [message]
  return Object.values(message).flatMap(strings)
}

function copyCodePoints(messages, face) {
  const points = new Set()
  for (const locale of messages) {
    for (const path of face.copy) {
      const message = path.split('.').reduce((node, key) => node?.[key], locale)
      if (message === undefined)
        throw new Error(`The home messages have no "${path}"`)
      for (const text of strings(message))
        for (const character of text) points.add(character.codePointAt(0))
    }
  }
  return points
}

async function subset(source, points, face, targetFormat) {
  return subsetFont(source, String.fromCodePoint(...points), {
    targetFormat,
    variationAxes: face.axes,
  })
}

/** The inclusive code point ranges that hold exactly these code points. */
function ranges(points) {
  const result = []
  for (const point of [...points].sort((a, b) => a - b)) {
    const last = result.at(-1)
    if (last && point === last[1] + 1) last[1] = point
    else result.push([point, point])
  }
  return result
}

/** The ranges that hold every code point outside these ranges. */
function complement(covered) {
  const result = []
  let next = 0
  for (const [from, to] of covered) {
    if (from > next) result.push([next, from - 1])
    next = to + 1
  }
  if (next <= LAST_CODE_POINT) result.push([next, LAST_CODE_POINT])
  return result
}

/**
 * One `@font-face` rule. A subset file's rule names the file's digest, which
 * ties the committed file to the ranges declared for it.
 */
function fontFace(face, file, unicodeRanges, content) {
  const hex = (point) => point.toString(16).toUpperCase()
  const terms = unicodeRanges.map(([from, to]) =>
    from === to ? `U+${hex(from)}` : `U+${hex(from)}-${hex(to)}`
  )
  const lines = []
  for (let index = 0; index < terms.length; index += 8)
    lines.push(`    ${terms.slice(index, index + 8).join(', ')}`)
  const digest = content
    ? `/* ${file.slice(SUBSET_DIR.length)} sha256 ${createHash('sha256').update(content).digest('hex')} */\n`
    : ''
  return `${digest}@font-face {
  font-family: '${face.family}';
  font-style: normal;
  font-weight: ${face.weight};
  font-display: swap;
  src: url(../${file.slice('src/'.length)}) format('woff2');
  unicode-range:
${lines.join(',\n')};
}
`
}

/** The generated files, keyed by their path under the frontend directory. */
async function build() {
  const messages = await homeMessages()
  const files = new Map()
  const rules = []

  for (const face of FACES) {
    const copy = copyCodePoints(messages, face)
    const wanted = new Set([...copy, ...ASCII])
    const covered = new Set()

    for (const [output, from] of Object.entries(face.files)) {
      const source = await readFile(new URL(FONT_DIR + from, frontend))
      // A font file is only declared for the characters it really holds.
      const outline = await subset(source, wanted, face, 'truetype')
      const points = [...new Face(new Blob(outline)).collectUnicodes()].filter(
        (point) => wanted.has(point) && !covered.has(point)
      )
      if (points.length === 0) continue
      points.forEach((point) => covered.add(point))
      const content = await subset(source, points, face, 'woff2')
      files.set(SUBSET_DIR + output, content)
      rules.push(fontFace(face, SUBSET_DIR + output, ranges(points), content))
    }

    // Copy without a glyph would make every visit download the complete font
    // and still show another typeface.
    const missing = [...copy].filter((point) => !covered.has(point))
    if (missing.length > 0)
      throw new Error(
        `${face.family} has no glyph for "${String.fromCodePoint(...missing)}"`
      )
    // The complete font is declared for everything else. ASCII it has no glyph
    // for is left out, so such a character goes straight to the next family.
    if (face.rest)
      rules.push(
        fontFace(face, FONT_DIR + face.rest, complement(ranges(wanted)))
      )
  }

  files.set(
    STYLESHEET,
    `/* Generated by scripts/home-font-subsets.mjs from the home copy. Do not edit:
   run \`bun run fonts:home\` after changing the copy these fonts render. */

${rules.join('\n')}`
  )
  return files
}

async function write(files) {
  await rm(new URL(SUBSET_DIR, frontend), { recursive: true, force: true })
  for (const [path, content] of files) {
    const target = fileURLToPath(new URL(path, frontend))
    await mkdir(dirname(target), { recursive: true })
    await writeFile(target, content)
  }
}

const files = await build()
await write(files)
for (const [path, content] of files)
  console.log(`${path}  ${Buffer.byteLength(content)} bytes`)
