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
 * The backend stops an update or rollback after 5 minutes and Nginx waits
 * 360 seconds, so the browser waits past the first but not the second.
 */
export const LONG_OPERATION_TIMEOUT_MS = 330_000
const RESTART_POLL_MS = 2_000
export const RESTART_DEADLINE_MS = 120_000

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

/**
 * What proves the restarted process is up: an update does not report the
 * installed release, so any other version counts; a rollback names its
 * target; a plain restart only changes the start time.
 */
export type RestartExpectation =
  | { kind: 'new-version' }
  | { kind: 'version'; version: string }
  | { kind: 'new-process' }

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
  expectation: RestartExpectation,
  current: ServerIdentity
) {
  switch (expectation.kind) {
    case 'new-version':
      return (
        Boolean(current.version?.trim()) &&
        !sameVersion(current.version, baseline.version)
      )
    case 'version':
      return sameVersion(current.version, expectation.version)
    case 'new-process':
      return (
        current.start_time !== undefined &&
        current.start_time !== baseline.start_time
      )
  }
}

/** Reads the status, cancelled at the deadline; a failed read yields null. */
async function readStatus(deadline: number, signal: AbortSignal) {
  const request = new AbortController()
  const stop = () => request.abort()
  const timer = setTimeout(stop, Math.max(0, deadline - Date.now()))
  signal.addEventListener('abort', stop, { once: true })
  try {
    return await publicApi.status(request.signal)
  } catch {
    // Reads fail while the process is down; the poll goes on.
    return null
  } finally {
    clearTimeout(timer)
    signal.removeEventListener('abort', stop)
  }
}

/**
 * Polls the public status until the restarted server is observed. The
 * deadline is fixed when the restart is sent; an answer that arrives after it
 * is not success.
 */
export async function watchServerRestart(
  baseline: ServerIdentity,
  expectation: RestartExpectation,
  deadline: number,
  signal: AbortSignal
): Promise<'restarted' | 'unknown'> {
  for (;;) {
    const left = deadline - Date.now()
    if (left <= 0) return 'unknown'
    await wait(Math.min(RESTART_POLL_MS, left), signal)
    if (Date.now() >= deadline) return 'unknown'
    const current = await readStatus(deadline, signal)
    if (signal.aborted) throw abortError()
    if (Date.now() >= deadline) return 'unknown'
    if (current && restarted(baseline, expectation, current)) return 'restarted'
  }
}
