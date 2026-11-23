import { api } from './client'
import {
  invalidResponse,
  isRecord,
  requiredBoolean,
  requiredString,
} from './contracts'
import { publicApi } from './public'

export type BuildType = 'release' | 'source'

export interface SystemVersion {
  version: string
  build_type: BuildType
}

export interface ReleaseInfo {
  name: string
  body: string
  published_at: string
  html_url: string
}

export interface UpdateCheck {
  current_version: string
  latest_version: string
  has_update: boolean
  build_type: BuildType
  cached: boolean
  /** The server text is not shown; the UI only needs to know a cache was used. */
  warning: boolean
  release_info: ReleaseInfo | null
}

export interface RollbackVersion {
  version: string
  published_at: string
  html_url: string
}

export type UpdateResult =
  | { kind: 'restart' }
  | { kind: 'current'; current_version: string; latest_version: string }

/**
 * The backend may spend up to 15 minutes downloading, verifying and replacing
 * the binary; the request must outlive that bound.
 */
export const LONG_OPERATION_TIMEOUT_MS = 16 * 60_000
const RESTART_POLL_MS = 2_000
const RESTART_DEADLINE_MS = 120_000

const VERSION = '/api/admin/system/version'
const CHECK = '/api/admin/system/check-updates'
const ROLLBACK_VERSIONS = '/api/admin/system/rollback-versions'
const UPDATE = '/api/admin/system/update'
const ROLLBACK = '/api/admin/system/rollback'
const RESTART = '/api/admin/system/restart'

function record(value: unknown, endpoint: string) {
  if (!isRecord(value)) invalidResponse(endpoint)
  return value
}

function buildType(value: unknown, endpoint: string): BuildType {
  if (value !== 'release' && value !== 'source') invalidResponse(endpoint)
  return value
}

function httpsUrl(value: unknown, endpoint: string) {
  const text = requiredString(value, endpoint, false)
  if (!text.startsWith('https://')) invalidResponse(endpoint)
  return text
}

function releaseInfo(value: unknown): ReleaseInfo | null {
  if (value === undefined || value === null) return null
  const row = record(value, CHECK)
  return {
    name: requiredString(row.name, CHECK),
    body: requiredString(row.body, CHECK),
    published_at: requiredString(row.published_at, CHECK),
    html_url: httpsUrl(row.html_url, CHECK),
  }
}

function warning(value: unknown) {
  if (value === undefined) return false
  return requiredString(value, CHECK).length > 0
}

function rollbackVersion(value: unknown): RollbackVersion {
  const row = record(value, ROLLBACK_VERSIONS)
  return {
    version: requiredString(row.version, ROLLBACK_VERSIONS, false),
    published_at: requiredString(row.published_at, ROLLBACK_VERSIONS),
    html_url: httpsUrl(row.html_url, ROLLBACK_VERSIONS),
  }
}

function updateResult(value: unknown): UpdateResult {
  const row = record(value, UPDATE)
  if (row.need_restart === true) return { kind: 'restart' }
  if (row.already_up_to_date !== true) invalidResponse(UPDATE)
  return {
    kind: 'current',
    current_version: requiredString(row.current_version, UPDATE, false),
    latest_version: requiredString(row.latest_version, UPDATE, false),
  }
}

export const systemUpdateApi = {
  async version(signal?: AbortSignal): Promise<SystemVersion> {
    const row = record(await api.get(VERSION, undefined, { signal }), VERSION)
    return {
      version: requiredString(row.version, VERSION, false),
      build_type: buildType(row.build_type, VERSION),
    }
  },
  async checkUpdates(
    force: boolean,
    signal?: AbortSignal
  ): Promise<UpdateCheck> {
    const row = record(await api.get(CHECK, { force }, { signal }), CHECK)
    return {
      current_version: requiredString(row.current_version, CHECK, false),
      latest_version: requiredString(row.latest_version, CHECK),
      has_update: requiredBoolean(row.has_update, CHECK),
      build_type: buildType(row.build_type, CHECK),
      cached: requiredBoolean(row.cached, CHECK),
      warning: warning(row.warning),
      release_info: releaseInfo(row.release_info),
    }
  },
  async rollbackVersions(signal?: AbortSignal): Promise<RollbackVersion[]> {
    const row = record(
      await api.get(ROLLBACK_VERSIONS, undefined, { signal }),
      ROLLBACK_VERSIONS
    )
    if (!Array.isArray(row.versions)) invalidResponse(ROLLBACK_VERSIONS)
    return row.versions.map(rollbackVersion)
  },
  async update(): Promise<UpdateResult> {
    return updateResult(
      await api.post(UPDATE, undefined, {
        timeoutMs: LONG_OPERATION_TIMEOUT_MS,
      })
    )
  },
  async rollback(version: string): Promise<{ version: string }> {
    const row = record(
      await api.post(
        ROLLBACK,
        { version },
        { timeoutMs: LONG_OPERATION_TIMEOUT_MS }
      ),
      ROLLBACK
    )
    if (row.need_restart !== true) invalidResponse(ROLLBACK)
    return { version: requiredString(row.version, ROLLBACK, false) }
  },
  async restart(): Promise<void> {
    const row = record(await api.post(RESTART, undefined, undefined), RESTART)
    if (row.restarting !== true) invalidResponse(RESTART)
  },
}

export interface ServerIdentity {
  version?: string
  start_time?: number
}

export function sameVersion(left?: string, right?: string) {
  const normalize = (value?: string) => value?.trim().replace(/^v/i, '') ?? ''
  return normalize(left) !== '' && normalize(left) === normalize(right)
}

function abortError() {
  return new DOMException('The restart watch was aborted', 'AbortError')
}

function wait(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal.aborted) return reject(abortError())
    const timer = setTimeout(() => {
      signal.removeEventListener('abort', stop)
      resolve()
    }, ms)
    function stop() {
      clearTimeout(timer)
      reject(abortError())
    }
    signal.addEventListener('abort', stop, { once: true })
  })
}

function restarted(
  baseline: ServerIdentity,
  target: string | null,
  current: ServerIdentity
) {
  // With a target, a new process still running the old binary is not success.
  if (target) return sameVersion(current.version, target)
  return (
    current.start_time !== undefined &&
    current.start_time !== baseline.start_time
  )
}

/**
 * Polls the public status until the restarted server is observed. Failed
 * reads are expected while the process is down and keep the poll going.
 */
export async function watchServerRestart(
  baseline: ServerIdentity,
  target: string | null,
  signal: AbortSignal
): Promise<'restarted' | 'unknown'> {
  const deadline = Date.now() + RESTART_DEADLINE_MS
  while (Date.now() < deadline) {
    await wait(RESTART_POLL_MS, signal)
    try {
      const current = await publicApi.status(signal)
      if (restarted(baseline, target, current)) return 'restarted'
    } catch {
      if (signal.aborted) throw abortError()
    }
  }
  return 'unknown'
}
