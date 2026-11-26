import type { CatalogOfferHealth } from '@/api/modelCatalog'

export type CatalogHealthTone = 'good' | 'warn' | 'bad' | 'neutral'
export type CatalogHealthIcon =
  'check' | 'pause' | 'limit' | 'recover' | 'down' | 'dash'

export interface ResolvedOfferHealth {
  state: string
  tone: CatalogHealthTone
  icon: CatalogHealthIcon
  /** Short 1–2 word label for compact corner badges beside billing mode. */
  badge: string
  /** Full primary status text for inline pills, detail panels and hover toasts. */
  primary: string
  /** Secondary quality detail when an availability issue coexists with quality downgrade. */
  secondary: string
}

/**
 * Single source of truth for projecting a user-scoped offer health snapshot
 * into tone, icon, short badge text and full status wording.
 */
export function resolveOfferHealth(
  health: CatalogOfferHealth | undefined,
  t: (key: string) => string
): ResolvedOfferHealth {
  if (!health) {
    return {
      state: 'unavailable',
      tone: 'neutral',
      icon: 'dash',
      badge: t('channelHealth.badges.unavailable'),
      primary: t('channelHealth.unavailable'),
      secondary: '',
    }
  }

  const hasQualityIssue =
    health.quality === 'degraded' || health.quality === 'severely_degraded'
  const secondary = hasQualityIssue
    ? t(`channelHealth.quality.${health.quality}`)
    : ''

  if (health.availability === 'paused') {
    return {
      state: 'paused',
      tone: 'bad',
      icon: 'pause',
      badge: t('channelHealth.badges.paused'),
      primary: t('channelHealth.availability.paused'),
      secondary,
    }
  }
  if (health.availability === 'limited') {
    return {
      state: 'limited',
      tone: 'warn',
      icon: 'limit',
      badge: t('channelHealth.badges.limited'),
      primary: t('channelHealth.availability.limited'),
      secondary,
    }
  }
  if (health.availability === 'recovering') {
    return {
      state: 'recovering',
      tone: 'warn',
      icon: 'recover',
      badge: t('channelHealth.badges.recovering'),
      primary: t('channelHealth.availability.recovering'),
      secondary,
    }
  }
  if (health.availability === 'unknown') {
    return {
      state: 'unknown',
      tone: 'neutral',
      icon: 'dash',
      badge: t('channelHealth.badges.unknown'),
      primary: t('channelHealth.availability.unknown'),
      secondary: '',
    }
  }

  // availability === 'no_restriction': quality decides visual tone (never green when unrated).
  if (health.quality === 'severely_degraded') {
    return {
      state: 'severely_degraded',
      tone: 'bad',
      icon: 'down',
      badge: t('channelHealth.badges.severely_degraded'),
      primary: t('channelHealth.quality.severely_degraded'),
      secondary: '',
    }
  }
  if (health.quality === 'degraded') {
    return {
      state: 'degraded',
      tone: 'warn',
      icon: 'down',
      badge: t('channelHealth.badges.degraded'),
      primary: t('channelHealth.quality.degraded'),
      secondary: '',
    }
  }
  if (health.quality === 'no_downgrade') {
    return {
      state: 'no_downgrade',
      tone: 'good',
      icon: 'check',
      badge: t('channelHealth.badges.no_downgrade'),
      primary: t('channelHealth.quality.no_downgrade'),
      secondary: '',
    }
  }

  return {
    state: 'unrated',
    tone: 'neutral',
    icon: 'dash',
    badge: t('channelHealth.badges.unrated'),
    primary: t('channelHealth.quality.unrated'),
    secondary: '',
  }
}
