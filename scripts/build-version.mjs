import { execFileSync } from 'node:child_process'
import { appendFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

const project = fileURLToPath(new URL('..', import.meta.url))
const source = execFileSync('git', ['-C', project, 'rev-parse', 'HEAD'], { encoding: 'utf8' }).trim()
if (!/^[0-9a-f]{40}$/.test(source)) throw new Error('A complete Git source revision is required')
const ref = process.env.GITHUB_REF || ''
const version = ref.startsWith('refs/tags/')
  ? ref.slice('refs/tags/'.length)
  : `0.0.0-dev.${source.slice(0, 12)}`
if (!/^[A-Za-z0-9][A-Za-z0-9.+-]*$/.test(version)) throw new Error('Invalid build version')
if (process.argv.includes('--github-env')) {
  if (!process.env.GITHUB_ENV) throw new Error('GITHUB_ENV is missing')
  appendFileSync(process.env.GITHUB_ENV, `VERSION=${version}\nSOURCE_SHA=${source}\n`)
}
process.stdout.write(process.argv.includes('--json')
  ? JSON.stringify({ version, source_sha: source }) + '\n'
  : version + '\n')
