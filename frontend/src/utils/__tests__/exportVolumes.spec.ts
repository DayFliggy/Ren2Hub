import { describe, expect, it } from 'vitest'

import { createExportVolumes } from '@/utils/exportVolumes'

// jsdom's Blob has no text(); FileReader is the browser API it does provide.
function text(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as string)
    reader.onerror = () => reject(reader.error)
    reader.readAsText(blob)
  })
}

const json = {
  prefix: '[',
  suffix: ']',
  separator: ',',
  mime: 'application/json',
}

function collect(volumeBytes: number) {
  const saved: { blob: Blob; part: number | null }[] = []
  const volumes = createExportVolumes(
    json,
    (blob, part) => saved.push({ blob, part }),
    volumeBytes
  )
  return { saved, volumes }
}

describe('export volumes', () => {
  it('keeps one unnumbered file while the rows fit', async () => {
    const { saved, volumes } = collect(64)
    volumes.append('1')
    volumes.append('2')
    expect(saved).toHaveLength(0)
    expect(volumes.finish()).toBe(1)
    expect(saved.map((file) => file.part)).toEqual([null])
    expect(await text(saved[0]!.blob)).toBe('[1,2]')
    expect(saved[0]!.blob.type).toBe('application/json')
  })

  it('saves a full file before the row that would overflow it', async () => {
    // "[" + "aaaa,bbbb" + "]" is 11 bytes; a third row does not fit in 12.
    const { saved, volumes } = collect(12)
    volumes.append('"aa"')
    volumes.append('"bb"')
    expect(saved).toHaveLength(0)
    volumes.append('"cc"')
    expect(saved).toHaveLength(1)
    expect(volumes.saved).toBe(1)
    volumes.append('"dd"')
    volumes.append('"ee"')
    expect(volumes.finish()).toBe(3)
    expect(saved.map((file) => file.part)).toEqual([1, 2, 3])
    expect(
      await Promise.all(
        saved.map(async (file) => JSON.parse(await text(file.blob)))
      )
    ).toEqual([['aa', 'bb'], ['cc', 'dd'], ['ee']])
  })

  it('counts UTF-8 bytes rather than UTF-16 characters', () => {
    // Each "中" is 3 bytes: [ + "中中" (8) + ] = 10 bytes.
    const { saved, volumes } = collect(10)
    volumes.append('"中中"')
    volumes.append('"x"')
    expect(saved).toHaveLength(1)
  })

  it('places a row larger than the volume in its own file', async () => {
    const { saved, volumes } = collect(4)
    volumes.append('"long row"')
    volumes.append('1')
    volumes.finish()
    expect(await text(saved[0]!.blob)).toBe('["long row"]')
    expect(await text(saved[1]!.blob)).toBe('[1]')
  })

  it('saves an empty but valid file when no row arrives', async () => {
    const { saved, volumes } = collect(16)
    expect(volumes.finish()).toBe(1)
    expect(await text(saved[0]!.blob)).toBe('[]')
  })
})
