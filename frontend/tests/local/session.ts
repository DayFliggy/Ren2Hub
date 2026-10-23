import {
  closeSync,
  constants,
  fstatSync,
  lstatSync,
  openSync,
  readFileSync,
} from 'node:fs'
import { dirname, resolve } from 'node:path'

interface LocalSession {
  baseURL: string
  project: string
  accounts: Record<
    'root' | 'user' | 'peer',
    { username: string; password: string }
  >
}

function rejectLinks(path: string) {
  let current = resolve(path)
  for (;;) {
    const metadata = lstatSync(current)
    if (
      metadata.isSymbolicLink() ||
      (metadata.isFile() && metadata.nlink !== 1)
    )
      throw new Error('Private paths must not contain symbolic or hard links')
    const parent = dirname(current)
    if (parent === current) return
    current = parent
  }
}

export function localSession(): { output: string; session: LocalSession } {
  const file = process.env.REN2HUB_LOCAL_SESSION
  const output = process.env.REN2HUB_LOCAL_OUTPUT
  if (!file || !output)
    throw new Error('Use the local browser runner with a built binary')
  rejectLinks(output)
  rejectLinks(file)
  const directory = lstatSync(output)
  if (
    !directory.isDirectory() ||
    (directory.mode & 0o777) !== 0o700 ||
    directory.uid !== process.getuid?.() ||
    dirname(resolve(file)) !== resolve(output)
  )
    throw new Error(
      'Browser output must be a private directory owned by the current user'
    )
  const descriptor = openSync(file, constants.O_RDONLY | constants.O_NOFOLLOW)
  try {
    const metadata = fstatSync(descriptor)
    if (
      !metadata.isFile() ||
      metadata.nlink !== 1 ||
      (metadata.mode & 0o777) !== 0o600 ||
      metadata.uid !== process.getuid?.()
    )
      throw new Error(
        'Browser credentials must be a private regular file without hard links'
      )
    return {
      output,
      session: JSON.parse(readFileSync(descriptor, 'utf8')) as LocalSession,
    }
  } finally {
    closeSync(descriptor)
  }
}
