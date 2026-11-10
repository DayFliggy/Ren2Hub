import { describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

import {
  consoleNavGroups,
  getAccessibleConsoleNavGroups,
  getConsoleRouteAccessMeta,
} from '@/constants/navigation/consoleNav'
import { useAppStore } from '@/stores/app'

/** Visible routes for a root administrator under one SidebarModulesAdmin. */
function sidebarRoutes(
  SidebarModulesAdmin: string,
  hasPermission: (resource: string, action: string) => boolean = () => true
): string[] {
  setActivePinia(createPinia())
  const app = useAppStore()
  app.status = { SidebarModulesAdmin }
  return getAccessibleConsoleNavGroups({
    isAdmin: true,
    isRoot: true,
    hasPermission,
    isSidebarModuleEnabled: app.isSidebarModuleEnabled,
  }).flatMap((group) => group.items.map((item) => item.route ?? ''))
}

describe('console navigation', () => {
  it('exposes the complete administration scaffold', () => {
    const adminGroup = consoleNavGroups.find((group) => group.key === 'admin')

    expect(adminGroup?.access).toBe('admin')

    expect(adminGroup?.items.map((item) => item.name)).toEqual([
      'channel-management',
      'models-admin',
      'ticket-management',
      'user-management',
      'redemption-management',
      'order-management',
      'task-plugins',
      'system-settings',
    ])
    expect(adminGroup?.items[0]).toEqual(
      expect.objectContaining({
        name: 'channel-management',
        route: 'channels',
      })
    )
    // Model management sits right after Channels.
    expect(adminGroup?.items[1]).toEqual(
      expect.objectContaining({
        name: 'models-admin',
        route: 'models-admin',
        labelKey: 'nav.adminModels',
      })
    )
    expect(adminGroup?.items[2]).toEqual(
      expect.objectContaining({
        name: 'ticket-management',
        route: 'ticket-management',
        permission: { resource: 'ticket', action: 'read' },
      })
    )
    expect(adminGroup?.items[3]).toEqual(
      expect.objectContaining({
        name: 'user-management',
        route: 'users',
      })
    )
    expect(adminGroup?.items).toContainEqual(
      expect.objectContaining({
        name: 'order-management',
        labelKey: 'nav.orderManagement',
        route: 'orders',
      })
    )

    // The real invariant: an item is live exactly when it has a route or an
    // explicit legacy-console href. Keying off positions instead would break
    // every time a page ships.
    adminGroup?.items.forEach((item) => {
      if (item.route || item.href) expect(item.disabled).toBeUndefined()
      else expect(item.disabled).toBe(true)
    })
  })

  it('shows the system-settings entry only to root users', () => {
    const adminItems = getAccessibleConsoleNavGroups({
      isAdmin: true,
      hasPermission: () => true,
    }).flatMap((group) => group.items)
    const rootItems = getAccessibleConsoleNavGroups({
      isAdmin: true,
      isRoot: true,
      hasPermission: () => true,
    }).flatMap((group) => group.items)

    expect(adminItems.some((item) => item.name === 'system-settings')).toBe(
      false
    )
    expect(
      rootItems.find((item) => item.name === 'system-settings')
    ).toMatchObject({
      route: 'system-settings-site',
      rootOnly: true,
    })
  })

  it('keeps discount configuration inside system settings without a duplicate sidebar entry', () => {
    const items = getAccessibleConsoleNavGroups({
      isAdmin: true,
      isRoot: true,
      hasPermission: () => true,
    }).flatMap((group) => group.items)
    expect(items.some((item) => item.name === 'topup-discount')).toBe(false)
    expect(items.find((item) => item.name === 'system-settings')).toMatchObject(
      {
        route: 'system-settings-site',
        rootOnly: true,
        feature: 'admin',
      }
    )
  })

  it('hides every administrator entry from ordinary users', () => {
    const ordinaryGroups = getAccessibleConsoleNavGroups({ isAdmin: false })
    const adminGroups = getAccessibleConsoleNavGroups({
      isAdmin: true,
      hasPermission: () => true,
    })
    const restrictedAdminGroups = getAccessibleConsoleNavGroups({
      isAdmin: true,
      hasPermission: () => false,
    })

    expect(ordinaryGroups.some((group) => group.key === 'admin')).toBe(false)
    expect(adminGroups.some((group) => group.key === 'admin')).toBe(true)
    expect(
      restrictedAdminGroups
        .flatMap((group) => group.items)
        .some((item) => item.route === 'channels')
    ).toBe(false)
    expect(getConsoleRouteAccessMeta('channels')).toEqual({
      requiresAdmin: true,
      requiresPermission: { resource: 'channel', action: 'read' },
    })
    expect(getConsoleRouteAccessMeta('ticket-management')).toEqual({
      requiresAdmin: true,
      requiresPermission: { resource: 'ticket', action: 'read' },
    })
    expect(getConsoleRouteAccessMeta('models')).toEqual({})
  })

  it('exposes wallet access without retired plan entries', () => {
    const accessibleItems = getAccessibleConsoleNavGroups({
      isAdmin: true,
      hasPermission: () => true,
    }).flatMap((group) => group.items)
    expect(accessibleItems.some((item) => item.name === 'subscription')).toBe(
      false
    )
    expect(
      accessibleItems.some((item) => item.name === 'plan-management')
    ).toBe(false)
    expect(
      accessibleItems.find((item) => item.name === 'wallet')
    ).toMatchObject({
      route: 'wallet',
      feature: 'wallet',
    })
  })

  it('keeps every route name unique across the whole navigation', () => {
    // Two entries sharing a route would make the active-item lookup in the
    // sidebar and the command palette resolve to whichever came first.
    const routes = consoleNavGroups
      .flatMap((group) => group.items)
      .flatMap((item) => (item.route ? [item.route] : []))

    expect(new Set(routes).size).toBe(routes.length)
  })

  it('disables entries explicitly disabled by backend capabilities', () => {
    const groups = getAccessibleConsoleNavGroups({
      isAdmin: false,
      featureStatus: (feature) =>
        feature === 'marketplace' ? 'disabled' : 'live',
    })
    const items = groups.flatMap((group) => group.items)

    expect(items.some((item) => item.route === 'market')).toBe(false)
    expect(items.find((item) => item.route === 'models')?.disabled).toBeFalsy()
  })

  it('hides only the modules SidebarModulesAdmin turns off', () => {
    const channelOff = sidebarRoutes(
      '{"admin":{"enabled":true,"channel":false}}'
    )
    expect(channelOff).not.toContain('channels')
    expect(channelOff).toContain('users')

    const consoleOff = sidebarRoutes('{"console":{"enabled":false}}')
    expect(consoleOff).not.toContain('keys')
    expect(consoleOff).not.toContain('logs')
    expect(consoleOff).toEqual(expect.arrayContaining(['tickets', 'models']))
    expect(consoleOff).toContain('channels')
  })

  it('fills a missing section or module from the all-on defaults', () => {
    const all = sidebarRoutes('')
    // Missing module: the admin section only turns user management off.
    expect(sidebarRoutes('{"admin":{"user":false}}')).toEqual(
      all.filter((route) => route !== 'users')
    )
    // Missing section: only the personal section is configured.
    expect(
      sidebarRoutes('{"personal":{"enabled":true,"topup":false}}')
    ).toEqual(all.filter((route) => route !== 'wallet'))
    expect(sidebarRoutes('{}')).toEqual(all)
  })

  it('shows every module and reports an option it cannot read', () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    const all = sidebarRoutes('')

    expect(sidebarRoutes('not-json')).toEqual(all)
    expect(sidebarRoutes('[]')).toEqual(all)
    expect(error).toHaveBeenCalledTimes(2)
    error.mockRestore()
  })

  it('never shows an entry the permission filter removed', () => {
    const routes = sidebarRoutes(
      '{"admin":{"enabled":true,"channel":true}}',
      (resource) => resource !== 'channel'
    )

    expect(routes).not.toContain('channels')
    expect(getConsoleRouteAccessMeta('channels')).toEqual({
      requiresAdmin: true,
      requiresPermission: { resource: 'channel', action: 'read' },
    })
  })
})
