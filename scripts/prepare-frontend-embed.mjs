import { cp, readFile, rm, stat } from 'node:fs/promises'
import { dirname, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'

const projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const source = resolve(projectRoot, 'frontend/dist')
const destination = resolve(projectRoot, 'frontend/embed-dist')
const index = await readFile(resolve(source, 'index.html'), 'utf8')
const manifest = JSON.parse(
  await readFile(resolve(source, '.vite/manifest.json'), 'utf8')
)
const entries = Object.values(manifest).filter((entry) => entry.isEntry)

if (index.includes('content="placeholder"') || entries.length === 0) {
  throw new Error('Vue production build is unavailable; run bun run build in frontend first')
}

for (const entry of entries) {
  if (typeof entry.file !== 'string' || !entry.file.startsWith('assets/')) {
    throw new Error('Vue manifest contains an invalid entry asset')
  }
  const asset = resolve(source, entry.file)
  if (!asset.startsWith(source + sep) || !(await stat(asset)).isFile()) {
    throw new Error('Vue entry asset is missing or outside dist')
  }
  if (!index.includes(`/${entry.file}`)) {
    throw new Error('Vue entry HTML must reference the root production assets')
  }
}

for (const entry of Object.values(manifest)) {
  for (const reference of [...(entry.imports ?? []), ...(entry.dynamicImports ?? [])]) {
    if (!Object.hasOwn(manifest, reference)) throw new Error(`Missing Vue manifest dependency: ${reference}`)
  }
  for (const file of [entry.file, ...(entry.css ?? []), ...(entry.assets ?? [])]) {
    if (typeof file !== 'string') throw new Error('Invalid Vue manifest asset')
    const asset = resolve(source, file)
    if (!asset.startsWith(source + sep) || !(await stat(asset)).isFile()) {
      throw new Error(`Missing Vue production asset: ${file}`)
    }
  }
}

await rm(destination, { recursive: true, force: true })
await cp(source, destination, { recursive: true })
console.log('Vue production assets prepared for Go embed')
