// Mirrors upstream lib/marketplace.ts. Ren2Hub resolves index paths to HTTPS
// on the index origin only (upstream also admits plain HTTP).
import type {
  MarketplaceIndex,
  MarketplaceDocument,
  MarketplaceSource,
  MarketplaceIndexVersion,
  MarketplacePlugin,
  MarketplacePluginIcon,
  TaskPluginListItem,
} from '../types'
import { parseProtocolClaims } from './plugin-meta-preview'
import { PluginSourceFetchError, fetchPluginBytes } from './plugin-url'
import { getPluginWebsite } from './plugin-website'

const MAX_INDEX_BYTES = 2 * 1024 * 1024

/** Fetched by the browser with the same bounded, credential-free request. */
export async function fetchMarketplaceIndex(
  indexUrl: string,
  signal?: AbortSignal
): Promise<MarketplaceIndex> {
  const bytes = await fetchPluginBytes(indexUrl, MAX_INDEX_BYTES, signal)
  const text = new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  return parseMarketplaceIndex(JSON.parse(text))
}

export const SUPPORTED_INDEX_VERSION = 1

/** The index kind remains task for task, provider and transform exports. */
export const SUPPORTED_PLUGIN_KIND = 'task'

/**
 * Resolves a version `path` against its index URL, so one index works behind
 * any raw prefix. Returns null when the result leaves the index origin.
 */
export function resolvePluginSourceUrl(
  indexUrl: string,
  path: string
): string | null {
  const trimmed = path.trim()
  if (!trimmed) return null
  let base: URL
  let resolved: URL
  try {
    base = new URL(indexUrl)
    resolved = new URL(trimmed, base)
  } catch {
    return null
  }
  if (
    base.protocol !== 'https:' ||
    resolved.origin !== base.origin ||
    resolved.username ||
    resolved.password
  )
    return null
  return resolved.toString()
}

/**
 * Validates an untrusted index into the display shape. Malformed entries are
 * skipped; admission still happens server-side on the compiled source.
 */
export function parseMarketplaceIndex(payload: unknown): MarketplaceIndex {
  if (!payload || typeof payload !== 'object')
    throw new Error('index is not an object')
  const raw = payload as Record<string, unknown>
  const indexVersion = Number(raw.indexVersion)
  if (!Number.isFinite(indexVersion))
    throw new Error('index is missing indexVersion')
  if (indexVersion > SUPPORTED_INDEX_VERSION)
    throw new Error(`unsupported indexVersion ${indexVersion}`)
  const plugins: MarketplacePlugin[] = []
  if (Array.isArray(raw.plugins)) {
    for (const entry of raw.plugins) {
      const plugin = parseMarketplacePlugin(entry)
      if (plugin) plugins.push(plugin)
    }
  }
  return {
    revoked: Array.isArray(raw.revoked)
      ? raw.revoked.flatMap((entry) => {
          if (
            !entry ||
            typeof entry !== 'object' ||
            typeof entry.sha256 !== 'string' ||
            !/^[a-f0-9]{64}$/i.test(entry.sha256.trim())
          )
            return []
          return [
            {
              sha256: entry.sha256.trim().toLowerCase(),
              key: typeof entry.key === 'string' ? entry.key : undefined,
              reason: parseDescription(entry.reason),
            },
          ]
        })
      : [],
    indexVersion,
    name: typeof raw.name === 'string' ? raw.name : '',
    plugins: plugins.sort((left, right) => {
      if (left.recommended !== right.recommended)
        return left.recommended ? -1 : 1
      if ((right.installs30d ?? 0) !== (left.installs30d ?? 0))
        return (right.installs30d ?? 0) - (left.installs30d ?? 0)
      const difference = (right.sortPriority ?? 0) - (left.sortPriority ?? 0)
      if (difference !== 0) return difference
      if (left.key === right.key) return 0
      return left.key < right.key ? -1 : 1
    }),
  }
}

function text(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function parseVersion(candidate: unknown): MarketplaceIndexVersion | null {
  if (!candidate || typeof candidate !== 'object') return null
  const raw = candidate as Record<string, unknown>
  const version = text(raw.version)
  const path = text(raw.path)
  const kind = text(raw.kind)
  if (!version || !path || (kind && kind !== SUPPORTED_PLUGIN_KIND)) return null
  return {
    version,
    path,
    sha256: typeof raw.sha256 === 'string' ? raw.sha256.trim() : undefined,
    minApiVersion: Number.isFinite(Number(raw.minApiVersion))
      ? Number(raw.minApiVersion)
      : undefined,
    kind: kind || undefined,
    readme: parseDocument(raw.readme),
    changelog: parseDocument(raw.changelog),
    allowedHosts: stringArray(raw.allowedHosts),
    baseUrl: text(raw.baseUrl) || undefined,
    auth: typeof raw.auth === 'string' ? raw.auth : undefined,
  }
}

function parseIcon(value: unknown): string | undefined {
  // A LobeHub name or the text scheme, exactly what meta.icon admits. Data
  // URIs and remote URLs are dropped so an index cannot become a beacon.
  const icon = text(value)
  if (!icon || icon.length > 128 || icon.startsWith('data:')) return undefined
  return icon.includes('://') ? undefined : icon
}

function parseIconFile(value: unknown): MarketplacePluginIcon | undefined {
  if (!value || typeof value !== 'object') return undefined
  const raw = value as Record<string, unknown>
  const path = text(raw.path)
  if (!/\.(svg|png)$/i.test(path)) return undefined
  return {
    path,
    sha256: typeof raw.sha256 === 'string' ? raw.sha256.trim() : undefined,
  }
}

function parseMarketplacePlugin(entry: unknown): MarketplacePlugin | null {
  if (!entry || typeof entry !== 'object') return null
  const raw = entry as Record<string, unknown>
  const key = text(raw.key)
  if (!key) return null
  const versions = (Array.isArray(raw.versions) ? raw.versions : [])
    .map(parseVersion)
    .filter((version): version is MarketplaceIndexVersion => version !== null)
  if (versions.length === 0) return null
  const declaredLatest = text(raw.latest)
  const latest = versions.some((entry) => entry.version === declaredLatest)
    ? declaredLatest
    : versions[0]!.version
  const protocols = parseProtocolClaims(raw.protocols)
  const sortPriority = raw.sortPriority
  return {
    key,
    code: /^\d{8}$/.test(text(raw.code)) ? text(raw.code) : undefined,
    recommended: raw.recommended === true,
    installs30d:
      typeof raw.installs30d === 'number' &&
      Number.isSafeInteger(raw.installs30d) &&
      raw.installs30d >= 0
        ? raw.installs30d
        : undefined,
    author:
      raw.author &&
      typeof raw.author === 'object' &&
      typeof (raw.author as Record<string, unknown>).login === 'string'
        ? {
            login: String((raw.author as Record<string, unknown>).login),
            url: getPluginWebsite((raw.author as Record<string, unknown>).url),
          }
        : undefined,
    repo: getPluginWebsite(raw.repo),
    name_i18n: parseDescription(raw.name_i18n),
    sortPriority:
      typeof sortPriority === 'number' &&
      Number.isInteger(sortPriority) &&
      sortPriority >= -2147483648 &&
      sortPriority <= 2147483647
        ? sortPriority
        : 0,
    website: getPluginWebsite(raw.website),
    name: typeof raw.name === 'string' && raw.name ? raw.name : key,
    icon: parseIcon(raw.icon),
    iconFile: parseIconFile(raw.iconFile),
    description: parseDescription(raw.description),
    channelTypes: numberArray(raw.channelTypes),
    models: stringArray(raw.models),
    protocols: typeof protocols === 'symbol' ? undefined : protocols,
    latest,
    versions,
  }
}

function parseDescription(
  value: unknown
): string | Record<string, string> | undefined {
  if (typeof value === 'string') return value
  if (!value || typeof value !== 'object' || Array.isArray(value))
    return undefined
  const mapped: Record<string, string> = {}
  for (const [locale, entry] of Object.entries(value)) {
    if (typeof entry === 'string') mapped[locale] = entry
  }
  return Object.keys(mapped).length > 0 ? mapped : undefined
}

function stringArray(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined
  return value.every((item) => typeof item === 'string' && item.length > 0)
    ? value
    : undefined
}

function numberArray(value: unknown): number[] | undefined {
  if (!Array.isArray(value)) return undefined
  const items = value.filter(
    (item): item is number => typeof item === 'number' && Number.isFinite(item)
  )
  return items.length > 0 ? items : undefined
}

export function findMarketplaceVersion(
  plugin: MarketplacePlugin,
  version: string
): MarketplaceIndexVersion | undefined {
  return plugin.versions.find((entry) => entry.version === version)
}

export type InstallState =
  | { status: 'not_installed' }
  | { status: 'up_to_date'; installedVersion: string }
  | { status: 'upgradable'; installedVersion: string; latestVersion: string }
  | { status: 'diverged'; installedVersion: string; latestVersion: string }

/**
 * `diverged` means the installed version is not listed by the index (a local
 * upload, or a source that rolled back): installing is not an upgrade.
 */
export function deriveInstallState(
  plugin: MarketplacePlugin,
  installed: TaskPluginListItem[]
): InstallState {
  const match = installed.find((item) => item.meta.key === plugin.key)
  if (!match) return { status: 'not_installed' }
  const installedVersion = match.meta.version
  if (installedVersion === plugin.latest)
    return { status: 'up_to_date', installedVersion }
  const known = plugin.versions.some(
    (entry) => entry.version === installedVersion
  )
  return {
    status: known ? 'upgradable' : 'diverged',
    installedVersion,
    latestVersion: plugin.latest,
  }
}

export type ReplaceReason = 'diverged' | 'downgrade'

/**
 * Ren2Hub-only guard: replacing a version the index does not list, or moving
 * to a version listed after (older than) the installed one, needs an explicit
 * confirmation. Order follows the index, whose versions are newest first.
 */
export function replaceConfirmationReason(
  plugin: MarketplacePlugin,
  state: InstallState,
  targetVersion: string
): ReplaceReason | null {
  if (state.status === 'not_installed') return null
  if (state.status === 'diverged') return 'diverged'
  const installed = plugin.versions.findIndex(
    (entry) => entry.version === state.installedVersion
  )
  const target = plugin.versions.findIndex(
    (entry) => entry.version === targetVersion
  )
  return installed >= 0 && target > installed ? 'downgrade' : null
}

/** Built-in version shown next to the marketplace latest. */
export function marketplaceBuiltInVersion(
  installed?: TaskPluginListItem
): string | undefined {
  if (!installed) return undefined
  if (installed.source === 'factory') return installed.meta.version
  return installed.factory_meta?.version
}

export function isStaleFactoryOverride(item: TaskPluginListItem): boolean {
  return (
    item.source === 'override_over_factory' &&
    item.factory_meta != null &&
    item.factory_meta.version !== item.meta.version
  )
}

/** A source is integrity-checked only when every listed version has a sha256. */
export function indexHasIntegrityHashes(index: MarketplaceIndex): boolean {
  return (
    index.plugins.length > 0 &&
    index.plugins.every((plugin) =>
      plugin.versions.every((version) => Boolean(version.sha256))
    )
  )
}

export const DEFAULT_MARKETPLACE_INDEX_URL =
  'https://www.newapi.ai/api/v1/plugins/index.json'

export const GITHUB_MARKETPLACE_INDEX_URL =
  'https://raw.githubusercontent.com/QuantumNous/new-api-plugins/main/index.json'

/** Both built-in indexes are maintained by the NewAPI project. */
export function isDefaultMarketplaceSource(indexUrl: string): boolean {
  const normalized = indexUrl.trim()
  return (
    normalized === DEFAULT_MARKETPLACE_INDEX_URL ||
    normalized === GITHUB_MARKETPLACE_INDEX_URL
  )
}

export const COMMUNITY_MARKETPLACE_INDEX_URL =
  'https://www.newapi.ai/api/v1/community/index.json'
export function marketplaceSourceKind(url: string) {
  return isDefaultMarketplaceSource(url)
    ? 'official'
    : url.trim() === COMMUNITY_MARKETPLACE_INDEX_URL
      ? 'community'
      : 'third_party'
}
function parseDocument(value: unknown): MarketplaceDocument | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    return undefined
  const raw = value as Record<string, unknown>
  if (
    !text(raw.path) ||
    (raw.sha256 !== undefined && !/^[a-f0-9]{64}$/i.test(text(raw.sha256)))
  )
    return undefined
  const translations: NonNullable<MarketplaceDocument['translations']> = {}
  if (raw.translations && typeof raw.translations === 'object')
    for (const [locale, entry] of Object.entries(raw.translations)) {
      const document = parseDocument(entry)
      if (document) translations[locale] = document
    }
  return {
    path: text(raw.path),
    sha256: text(raw.sha256) || undefined,
    sourceUrl: getPluginWebsite(raw.sourceUrl),
    translations,
  }
}
export async function findPluginByCode(
  code: string,
  sources: MarketplaceSource[],
  signal?: AbortSignal
) {
  const wanted = code.trim()
  if (!/^\d{8}$/.test(wanted)) throw new Error('Enter an 8-digit install code')
  let failure: unknown
  for (const source of sources.filter((item) =>
    isDefaultMarketplaceSource(item.index_url)
  )) {
    try {
      const index = await fetchMarketplaceIndex(source.index_url, signal)
      const plugin = index.plugins.find((item) => item.code === wanted)
      if (plugin) return { source, plugin }
    } catch (cause) {
      signal?.throwIfAborted()
      failure = cause
    }
  }
  const source = sources.find(
    (item) => item.index_url === COMMUNITY_MARKETPLACE_INDEX_URL
  )
  if (source) {
    const url = new URL('resolve', COMMUNITY_MARKETPLACE_INDEX_URL)
    url.searchParams.set('code', wanted)
    try {
      const index = await fetchMarketplaceIndex(url.href, signal)
      const plugin = index.plugins[0]
      if (plugin) return { source, plugin }
    } catch (cause) {
      signal?.throwIfAborted()
      if (!(cause instanceof PluginSourceFetchError && cause.status === 404))
        throw cause
    }
  }
  if (failure) throw failure
  return null
}

export function isCanonicalLanguageTag(value: string): boolean {
  try {
    return Intl.getCanonicalLocales(value)[0] === value
  } catch {
    return false
  }
}
