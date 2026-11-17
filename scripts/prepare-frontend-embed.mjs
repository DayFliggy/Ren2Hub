import { cp, readdir, readFile, rm, stat, writeFile } from 'node:fs/promises'
import { dirname, extname, resolve, sep } from 'node:path'
import { fileURLToPath } from 'node:url'
import { brotliCompressSync, constants } from 'node:zlib'

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

// middleware.ServeFrontendBrotliFiles sends <file>.br to clients that accept
// br, so text files are compressed once here instead of in the server.
const brotliExtensions = new Set(['.js', '.mjs', '.css', '.svg', '.json', '.webmanifest', '.txt'])
let brotliFiles = 0
for (const entry of await readdir(destination, { recursive: true, withFileTypes: true })) {
  if (!entry.isFile() || !brotliExtensions.has(extname(entry.name))) continue
  const file = resolve(entry.parentPath, entry.name)
  const raw = await readFile(file)
  const compressed = brotliCompressSync(raw, {
    params: {
      [constants.BROTLI_PARAM_QUALITY]: 11,
      [constants.BROTLI_PARAM_SIZE_HINT]: raw.length,
    },
  })
  if (compressed.length >= raw.length) continue
  await writeFile(`${file}.br`, compressed)
  brotliFiles++
}
console.log(`Vue production assets prepared for Go embed (${brotliFiles} brotli files)`)
