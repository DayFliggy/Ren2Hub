// Localized from upstream lib/marketplace-install.ts for Ren2Hub Vue.
// Preserves Ren2Hub HTTPS-only origin resolution, API version and AST metadata
// identity checks, AbortSignal cancellation, and typed errors translated via vue-i18n.
import {
  activateTaskPlugin,
  installMarketplacePlugin,
  previewTaskPlugin,
} from '../api'
import type {
  MarketplaceIndexVersion,
  MarketplacePlugin,
  MarketplaceSource,
  TaskPluginDetail,
} from '../types'
import { resolvePluginSourceUrl } from './marketplace'
import { fetchPluginIconDataUri } from './plugin-icon-file'
import {
  computeSourceSha256,
  fetchPluginSourceText,
  PluginSourceFetchError,
} from './plugin-url'

export type MarketplaceInstallFailureReason =
  | 'invalid_path'
  | 'integrity_mismatch'
  | 'unsupported_api'
  | 'identity_mismatch'

export class MarketplaceInstallError extends Error {
  constructor(public readonly reason: MarketplaceInstallFailureReason) {
    super(reason)
    this.name = 'MarketplaceInstallError'
  }
}

export class MarketplaceActivationError extends Error {
  constructor(message: string, options?: { cause?: unknown }) {
    super(message, options)
    this.name = 'MarketplaceActivationError'
  }
}

export type MarketplaceReleaseDownload = {
  text: string
  digest: string | null
  icon: string | null
}

type TranslateFn = (key: string, params?: Record<string, unknown>) => string

export async function downloadMarketplaceRelease(
  source: MarketplaceSource,
  plugin: MarketplacePlugin,
  entry: MarketplaceIndexVersion,
  signal?: AbortSignal
): Promise<MarketplaceReleaseDownload> {
  const url = resolvePluginSourceUrl(source.index_url, entry.path)
  if (!url) throw new MarketplaceInstallError('invalid_path')
  const text = await fetchPluginSourceText(url, signal)
  const iconUrl = plugin.iconFile
    ? resolvePluginSourceUrl(source.index_url, plugin.iconFile.path)
    : null
  const [digest, icon] = await Promise.all([
    computeSourceSha256(text),
    iconUrl
      ? fetchPluginIconDataUri(iconUrl, {
          sha256: plugin.iconFile?.sha256,
          signal,
        }).catch(() => {
          signal?.throwIfAborted()
          return null
        })
      : Promise.resolve(null),
  ])
  signal?.throwIfAborted()
  return { text, digest, icon }
}

/**
 * Ren2Hub guard shared by single-plugin and bulk marketplace installs:
 * blocks unsupported API versions and source metadata identity mismatches.
 */
export function validateMarketplaceReleaseIdentity(
  plugin: MarketplacePlugin,
  entry: MarketplaceIndexVersion,
  previewMeta: Record<string, unknown> | null
): MarketplaceInstallFailureReason | null {
  if ((entry.minApiVersion ?? 1) > 1) return 'unsupported_api'
  if (
    !previewMeta ||
    previewMeta.key !== plugin.key ||
    previewMeta.version !== entry.version
  )
    return 'identity_mismatch'
  return null
}

export async function installMarketplaceRelease(
  source: MarketplaceSource,
  plugin: MarketplacePlugin,
  entry: MarketplaceIndexVersion,
  download: MarketplaceReleaseDownload,
  options?: {
    previewMeta?: Record<string, unknown> | null
    signal?: AbortSignal
    onUploaded?: (detail: TaskPluginDetail) => void
  }
): Promise<TaskPluginDetail> {
  if ((entry.minApiVersion ?? 1) > 1)
    throw new MarketplaceInstallError('unsupported_api')
  if (
    entry.sha256 &&
    download.digest &&
    entry.sha256.toLowerCase() !== download.digest.toLowerCase()
  )
    throw new MarketplaceInstallError('integrity_mismatch')

  const previewMeta =
    options?.previewMeta !== undefined
      ? options.previewMeta
      : await previewTaskPlugin(download.text, options?.signal)
          .then((preview) => preview.meta)
          .catch((error) => {
            options?.signal?.throwIfAborted()
            void error
            return null
          })
  options?.signal?.throwIfAborted()
  const identityIssue = validateMarketplaceReleaseIdentity(
    plugin,
    entry,
    previewMeta
  )
  if (identityIssue) throw new MarketplaceInstallError(identityIssue)

  const detail = await installMarketplacePlugin({
    source: download.text,
    sourceSha256: entry.sha256,
    remark: `${source.name} v${entry.version}`,
    icon: download.icon ?? undefined,
    marketplaceIndexUrl: source.index_url,
  })
  options?.onUploaded?.(detail)
  if (!detail.plugin || detail.plugin.active) return detail

  try {
    const notice = await activateTaskPlugin(
      detail.meta.key,
      detail.meta.version
    )
    return { ...detail, pricing_issues: notice?.pricing_issues ?? [] }
  } catch (error) {
    throw new MarketplaceActivationError(
      error instanceof Error ? error.message : String(error),
      { cause: error }
    )
  }
}

export function formatMarketplaceIndexError(
  error: unknown,
  t: TranslateFn
): string {
  if (error instanceof PluginSourceFetchError && error.reason === 'not_found')
    return t('taskPlugins.indexHttp', { status: error.status ?? 0 })
  return error instanceof Error ? error.message : String(error)
}

export function formatMarketplaceInstallError(
  error: unknown,
  t: TranslateFn
): string {
  if (error instanceof MarketplaceInstallError) {
    switch (error.reason) {
      case 'invalid_path':
        return t('taskPlugins.pathOutside')
      case 'integrity_mismatch':
        return t('taskPlugins.integrityFailed')
      case 'unsupported_api':
        return t('taskPlugins.unsupported')
      case 'identity_mismatch':
        return t('taskPlugins.identityMismatch')
    }
  }
  if (error instanceof PluginSourceFetchError) {
    return error.reason === 'too_large'
      ? t('taskPlugins.sourceTooLarge')
      : t('taskPlugins.fetchSourceFailed')
  }
  return error instanceof Error ? error.message : String(error)
}
