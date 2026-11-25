export interface VisualRoute {
  name: string
  path: string
  guestOnly?: boolean
}

export const VISUAL_ROUTES: VisualRoute[] = [
  { name: 'home', path: '/' },
  { name: 'setup', path: '/setup' },
  { name: 'sign-in', path: '/sign-in', guestOnly: true },
  { name: 'sign-up', path: '/sign-up', guestOnly: true },
  { name: 'reset', path: '/forgot-password', guestOnly: true },
  { name: 'not-found', path: '/visual-regression-not-found' },
  { name: 'about', path: '/about' },
  { name: 'pricing', path: '/pricing' },
  { name: 'rankings', path: '/rankings' },
  { name: 'privacy-policy', path: '/privacy-policy' },
  { name: 'user-agreement', path: '/user-agreement' },
  ...[401, 403, 404, 500, 503].map((status) => ({
    name: `status-${status}`,
    path: `/${status}`,
  })),
  { name: 'metadata', path: '/models/metadata' },
  { name: 'vendors', path: '/models/vendors' },
  { name: 'model-pricing', path: '/models/pricing' },
  {
    name: 'prefill-groups',
    path: '/models/metadata?manage=prefill-groups',
  },
  { name: 'ops-monitor', path: '/ops' },
  { name: 'system-info-redirect', path: '/system-info' },
  ...[
    'site',
    'auth',
    'billing',
    'models',
    'security',
    'content',
    'operations',
  ].map((domain) => ({
    name: `settings-${domain}`,
    path: `/system-settings/${domain}`,
  })),
  { name: 'drawing-logs', path: '/usage-logs/drawing' },
  { name: 'task-logs', path: '/usage-logs/task' },
  { name: 'quota-logs', path: '/usage-logs/quota' },
  { name: 'dashboard', path: '/dashboard' },
  { name: 'activity', path: '/activity' },
  { name: 'models', path: '/models' },
  { name: 'keys', path: '/keys' },
  { name: 'logs', path: '/usage-logs' },
  { name: 'audit-logs', path: '/usage-logs/audit' },
  { name: 'channels', path: '/channels' },
  { name: 'users', path: '/users' },
  { name: 'redemption', path: '/redemption-codes' },
  { name: 'orders', path: '/orders' },
  { name: 'tickets', path: '/tickets' },
  { name: 'ticket-detail', path: '/tickets/1' },
  { name: 'wallet', path: '/wallet' },
  { name: 'invite', path: '/invite' },
  { name: 'settings', path: '/settings' },
  { name: 'security', path: '/security' },
  { name: 'profile', path: '/profile' },
]
