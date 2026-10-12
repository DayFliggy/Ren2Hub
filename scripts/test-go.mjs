import { spawnSync } from 'node:child_process'

const environment = { ...process.env, GOWORK: 'off' }

function go(args, capture = false) {
  const result = spawnSync('go', args, {
    env: environment,
    encoding: 'utf8',
    stdio: capture ? 'pipe' : 'inherit',
    maxBuffer: 16 * 1024 * 1024,
  })
  if (result.error) throw result.error
  if (result.status !== 0) {
    if (capture) process.stderr.write(result.stderr + result.stdout)
    process.exit(result.status || 1)
  }
  return result.stdout
}

const packages = go(['list', './...'], true).trim().split(/\r?\n/)
const batchedPackages = packages.filter((name) => name.endsWith('/controller') || name.endsWith('/router'))
const extra = process.argv.slice(2)
if (!extra.includes('--controller-only')) {
  go(['test', '-timeout', '60s', ...extra, ...packages.filter((name) => !batchedPackages.includes(name))])
}
for (const name of batchedPackages) {
  if (extra.includes('--controller-only') && !name.endsWith('/controller')) continue
  const tests = go(['test', '-list', '.', name], true)
    .split(/\r?\n/).filter((name) => /^(Test|Example|Fuzz)\w+$/.test(name))
  if (!tests.length) throw new Error(`Test inventory is empty: ${name}`)
  // Password hashing and gateway integration fixtures make these packages
  // exceed one minute cumulatively. Run the complete inventory in bounded
  // batches without relaxing the timeout or dropping slower test cases.
  const args = extra.filter((arg) => arg !== '--controller-only')
  for (let start = 0; start < tests.length; start += 20) {
    const batch = tests.slice(start, start + 20)
    go(['test', '-timeout', '60s', ...args, name, '-run', `^(${batch.join('|')})$`])
  }
}
