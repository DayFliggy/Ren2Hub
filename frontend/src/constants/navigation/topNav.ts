/**
 * Single source of truth for the console top navigation.
 * Consumed by TopNavMenu (desktop) and the ConsoleTopbar mobile strip.
 */
import type { FeatureStatus } from '@/api/public'
import { consoleEntryRoute } from './consoleNav'

export type TopNavName =
  'activities' | 'dashboard' | 'ops' | 'console' | 'alchemy' | 'community'

export interface TopNavChild {
  name: string
  labelKey: string
  route?: string
  disabled?: boolean
  icon?: string
}

export interface TopNavItem {
  name: TopNavName
  labelKey: string
  route?: string
  /** Shown disabled when the backend reports this capability disabled. */
  feature?: string
  /** Restricts the entry to administrators (role >= 10). */
  access?: 'admin' | 'root'
  /** The mobile strip has no dropdowns, so menu-only groups stay desktop. */
  desktopOnly?: boolean
  disabled?: boolean
  children?: TopNavChild[]
}

export interface TopNavAccessContext {
  isAdmin: boolean
  isRoot?: boolean
  surface: 'desktop' | 'mobile'
  featureStatus: (feature: string) => FeatureStatus
}

export const topNavItems: readonly TopNavItem[] = [
  { name: 'activities', labelKey: 'nav.activities', route: 'activity' },
  { name: 'dashboard', labelKey: 'nav.dashboard', route: 'dashboard' },
  {
    name: 'ops',
    labelKey: 'nav.opsMonitor',
    route: 'ops-monitor',
    feature: 'admin',
    access: 'root',
  },
  // Direct link into the workspace layout; its sub-nav lives in the sidebar.
  { name: 'console', labelKey: 'nav.console', route: consoleEntryRoute },
  // Permanent placeholder with no pages or capability behind it.
  { name: 'alchemy', labelKey: 'nav.alchemy', disabled: true },
  {
    name: 'community',
    labelKey: 'nav.community',
    desktopOnly: true,
    children: [{ name: 'docs', labelKey: 'nav.docs', disabled: true }],
  },
]

export function getVisibleTopNavItems(
  context: TopNavAccessContext
): TopNavItem[] {
  return topNavItems
    .filter(
      (item) =>
        (item.access !== 'admin' || context.isAdmin) &&
        (item.access !== 'root' || context.isRoot === true) &&
        (context.surface === 'desktop' || !item.desktopOnly)
    )
    .map((item) => ({
      ...item,
      disabled:
        item.disabled ||
        (item.feature !== undefined &&
          context.featureStatus(item.feature) === 'disabled'),
    }))
}

/** Route guards derive from the same access rule as the visible entry. */
export function getTopNavRouteMeta<
  N extends Exclude<TopNavName, 'alchemy' | 'community'>,
>(
  name: N
): {
  topNav: N
  feature?: string
  requiresAdmin?: true
  requiresRoot?: true
} {
  const item = topNavItems.find((candidate) => candidate.name === name)
  return {
    topNav: name,
    ...(item?.feature ? { feature: item.feature } : {}),
    ...(item?.access === 'admin' ? { requiresAdmin: true as const } : {}),
    ...(item?.access === 'root' ? { requiresRoot: true as const } : {}),
  }
}
