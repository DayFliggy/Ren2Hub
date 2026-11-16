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
    /** Routes sharing a value are tabs of one page under a common header. */
    tabGroup?: string
    topNav?: 'activities' | 'dashboard' | 'ops' | 'console'
    setupRoute?: boolean
    setupError?: boolean
    messageDomain?: 'auth' | 'console'
  }
}
