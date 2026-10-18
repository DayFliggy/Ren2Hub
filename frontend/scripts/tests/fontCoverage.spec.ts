import { createHash } from 'node:crypto'
import { readFileSync, readdirSync } from 'node:fs'
import { dirname, extname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const supplement = 'src/assets/fonts/noto-sans-ui'
const cssPath = resolve(frontend, 'src/styles/fonts.css')
const read = (path: string) => readFileSync(resolve(frontend, path), 'utf8')
const sha256 = (path: string) =>
  createHash('sha256')
    .update(readFileSync(resolve(frontend, path)))
    .digest('hex')
type Range = [number, number]
interface FontAttestation {
  file: string
  sha256: string
  cmap_ranges: Range[]
  axes: Record<string, Range>
}
const manifest = JSON.parse(read(`${supplement}/coverage.json`)) as {
  fonts: FontAttestation[]
}
const source = JSON.parse(read(`${supplement}/source.json`)) as {
  css_sha256: string
  font_sha256: string
  license_sha256: string
  requested_codepoints: Range[]
}
const inRanges = (point: number, ranges: Range[]) =>
  ranges.some(([start, end]) => point >= start && point <= end)

function chineseCorpus(directory: string): Set<number> {
  const result = new Set<number>()
  const extensions = new Set(['.ts', '.vue', '.css', '.html', '.json', '.svg'])
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    const path = resolve(directory, entry.name)
    if (entry.isDirectory()) {
      if (
        entry.name !== '__tests__' &&
        path !== resolve(frontend, 'src/assets/fonts')
      ) {
        for (const point of chineseCorpus(path)) result.add(point)
      }
      continue
    }
    if (!extensions.has(extname(path)) || /\.(spec|test)\.ts$/.test(path))
      continue
    const content = readFileSync(path, 'utf8').replace(
      /\\u(?:\{([0-9a-fA-F]+)\}|([0-9a-fA-F]{4}))/g,
      (_, extended: string, basic: string) =>
        String.fromCodePoint(parseInt(extended || basic, 16))
    )
    for (const char of content.matchAll(
      /[\u2e80-\u303f\u31c0-\u31ef\u3400-\u9fff\uf900-\ufaff\uff00-\uffef\u{20000}-\u{323af}]/gu
    )) {
      result.add(char[0].codePointAt(0)!)
    }
  }
  return result
}

function usableFonts() {
  return [
    ...readFileSync(cssPath, 'utf8').matchAll(/@font-face\s*\{([^}]+)\}/g),
  ]
    .map((match) => match[1]!)
    .filter((block) => block.includes("font-family: 'Ren2NotoSansSC'"))
    .map((block) => {
      const asset = block.match(/src:\s*url\(([^)]+)\)/)![1]!
      const path = resolve(dirname(cssPath), asset)
      const font = manifest.fonts.find(
        (record) => resolve(frontend, record.file) === path
      )
      expect(font, `Missing actual cmap attestation for ${asset}`).toBeDefined()
      const declaration = block.match(/unicode-range:\s*([^;]+);/i)?.[1]
      const ranges = declaration
        ? [...declaration.matchAll(/U\+([0-9a-f]+)(?:-([0-9a-f]+))?/gi)].map(
            ([, start, end]) =>
              [parseInt(start!, 16), parseInt(end || start!, 16)] as Range
          )
        : null
      return {
        font: font!,
        ranges,
        weight: block.match(/font-weight:\s*([^;]+);/)![1]!.trim(),
      }
    })
}

describe('self-hosted Chinese font coverage', () => {
  it('covers every production Chinese character with an attested glyph and an active CSS range', () => {
    const fonts = usableFonts()
    expect(fonts.length).toBeGreaterThan(0)
    for (const font of manifest.fonts) {
      expect(
        sha256(font.file),
        `Changed font needs a fresh cmap attestation: ${font.file}`
      ).toBe(font.sha256)
    }
    const required = chineseCorpus(resolve(frontend, 'src'))
    expect(required.size).toBeGreaterThan(1000)
    // Browser font matching happens before Unicode selection. A variable
    // range beside static faces can exclude an otherwise valid subset.
    for (const weight of new Set(fonts.map((font) => font.weight))) {
      const matching = fonts.filter((font) => font.weight === weight)
      const missing = [...required].filter(
        (point) =>
          !matching.some(
            ({ font, ranges }) =>
              inRanges(point, font.cmap_ranges) &&
              (!ranges || inRanges(point, ranges))
          )
      )
      expect(
        missing.map(
          (point) => `${String.fromCodePoint(point)} U+${point.toString(16)}`
        ),
        `Missing glyphs at weight ${weight}; run scripts/verify-font-coverage.py --update`
      ).toEqual([])
    }
  })

  it('preserves the official variable font, source response and full OFL attribution', () => {
    expect(sha256(`${supplement}/ui-supplement.woff2`)).toBe(source.font_sha256)
    expect(sha256(`${supplement}/source-response.txt`)).toBe(source.css_sha256)
    expect(sha256(`${supplement}/OFL.txt`)).toBe(source.license_sha256)
    expect(sha256('public/licenses/noto-sans-sc-OFL.txt')).toBe(
      source.license_sha256
    )
    expect(read(`${supplement}/OFL.txt`)).toContain(
      'SIL OPEN FONT LICENSE Version 1.1'
    )
    expect(read(`${supplement}/OFL.txt`)).toContain('Copyright 2014-2021 Adobe')
    const font = manifest.fonts.find((record) =>
      record.file.endsWith('/ui-supplement.woff2')
    )!
    expect(font.axes.wght).toEqual([100, 900])
    for (const [start, end] of source.requested_codepoints) {
      for (let point = start; point <= end; point++) {
        expect(
          inRanges(point, font.cmap_ranges),
          `Missing requested U+${point.toString(16)}`
        ).toBe(true)
      }
    }
  })

  it('keeps display and monospace Chinese fallback self-hosted in both themes', () => {
    for (const [file, properties] of [
      ['tokens.css', ['--font-display', '--font-mono']],
      ['tailwind.css', ['--font-serif', '--font-mono']],
      ['home.css', ['--home-font-display', '--home-font-stamp']],
    ] as const) {
      const css = read(`src/styles/${file}`)
      for (const property of properties) {
        const declarations = [
          ...css.matchAll(new RegExp(`${property}:([^;]+);`, 'g')),
        ]
        expect(declarations.length).toBeGreaterThan(0)
        for (const [, chain] of declarations) {
          const families = chain!
            .split(',')
            .map((family) => family.trim().replaceAll("'", ''))
          const fallback = families.indexOf('Ren2NotoSansSC')
          expect(fallback, `${file} ${property}`).toBeGreaterThan(0)
          expect(
            families
              .slice(0, fallback)
              .every((family) => family.startsWith('Ren2'))
          ).toBe(true)
        }
      }
    }
  })
})
