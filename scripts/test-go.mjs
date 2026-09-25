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
const controller = packages.find((name) => name.endsWith('/controller'))
const extra = process.argv.slice(2)
if (!extra.includes('--controller-only')) {
  go(['test', '-timeout', '60s', ...extra, ...packages.filter((name) => name !== controller)])
}
if (controller) {
  const tests = go(['test', '-list', '.', controller], true)
    .split(/\r?\n/).filter((name) => /^(Test|Example|Fuzz)\w+$/.test(name))
  if (!tests.length) throw new Error('Controller test inventory is empty')
  // Security fixtures intentionally use production password hashing. Split the
  // complete inventory so their accumulated time does not relax any timeout.
  const args = extra.filter((arg) => arg !== '--controller-only')
  for (let start = 0; start < tests.length; start += 20) {
    const batch = tests.slice(start, start + 20)
    go(['test', '-timeout', '60s', ...args, controller, '-run', `^(${batch.join('|')})$`])
  }
}
