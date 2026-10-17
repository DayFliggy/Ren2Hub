import { execFileSync } from 'node:child_process'
import { lstatSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

export default async function preflight() {
  for (const key of ['NATIVE_LIVE_TARGET', 'NATIVE_LIVE_CREDENTIALS']) {
    const path = process.env[key]
    if (!path) throw new Error(`${key} is required`)
    const stat = lstatSync(path)
    if (!stat.isFile() || stat.nlink !== 1 || (stat.mode & 0o777) !== 0o600)
      throw new Error(`${key} must be a private regular file (0600)`)
  }
  // Reuse the candidate verifier: it checks container/network namespace,
  // source identity, database/Redis targets, native schema and no SQLite.
  execFileSync(
    'python3',
    [
      '-c',
      [
        'import os',
        'from ren2hub_acceptance import configure_target, verify_preview_database',
        "configure_target(os.environ['NATIVE_LIVE_TARGET'])",
        "verify_preview_database(os.environ['NATIVE_LIVE_REVISION'])",
      ].join('\n'),
    ],
    {
      env: {
        ...process.env,
        PYTHONPATH: fileURLToPath(
          new URL('../../../../scripts/server', import.meta.url)
        ),
      },
      stdio: 'pipe',
      timeout: 60_000,
    }
  )
  const baseURL = process.env.NATIVE_LIVE_URL!
  const status = await fetch(`${baseURL}/api/status`).then((response) =>
    response.json()
  )
  if (
    !status.success ||
    status.data?.version !== process.env.NATIVE_LIVE_REVISION
  )
    throw new Error('Candidate HTTP revision mismatch')
  const html = await fetch(baseURL).then((response) => response.text())
  if (html.includes('/@vite/client') || !/\/assets\/[^"']+\.js/.test(html))
    throw new Error(
      'Live tests require built Vue assets, not a development server'
    )
}
