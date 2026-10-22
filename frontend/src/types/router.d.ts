import 'vue-router'

declare module 'vue-router' {
  interface RouteMeta {
    public?: boolean
    guestOnly?: boolean
    requiresAuth?: boolean
    requiresAdmin?: boolean
    requiresRoot?: boolean
    requiresPermission?: { resource: string; action: string }
    noPageScroll?: boolean
    wide?: boolean
    protected?: boolean
    feature?: string
    nav?: string
    topNav?: 'activities' | 'dashboard' | 'ops' | 'console'
    setupRoute?: boolean
    setupError?: boolean
    messageDomain?: 'auth' | 'console'
  }
}
