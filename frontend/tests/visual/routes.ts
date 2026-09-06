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
  ...['metadata', 'vendors', 'prefill-groups', 'deployments'].map(
    (section) => ({ name: section, path: `/models/${section}` })
  ),
  { name: 'system-info', path: '/system-info' },
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
  { name: 'dashboard', path: '/dashboard' },
  { name: 'activity', path: '/activity' },
  { name: 'models', path: '/models' },
  { name: 'market', path: '/market' },
  { name: 'keys', path: '/keys' },
  { name: 'logs', path: '/usage-logs' },
  { name: 'operation-logs', path: '/usage-logs/operations' },
  { name: 'channels', path: '/channels' },
  { name: 'users', path: '/users' },
  { name: 'redemption', path: '/redemption-codes' },
  { name: 'plan-management', path: '/plan-management' },
  { name: 'orders', path: '/orders' },
  { name: 'tickets', path: '/tickets' },
  { name: 'ticket-detail', path: '/tickets/1' },
  { name: 'wallet', path: '/wallet' },
  { name: 'subscription', path: '/subscription' },
  { name: 'invite', path: '/invite' },
  { name: 'invoice', path: '/invoice' },
  { name: 'settings', path: '/settings' },
  { name: 'profile', path: '/profile' },
  { name: 'farm', path: '/farm' },
  { name: 'bigame', path: '/bigame' },
  { name: 'lab-chat', path: '/lab/chat' },
  { name: 'lab-chat-session', path: '/lab/chat/c-1' },
  { name: 'lab-studio', path: '/lab/studio' },
  { name: 'lab-assets', path: '/lab/assets' },
  { name: 'lab-notes', path: '/lab/notes' },
  { name: 'lab-plugins', path: '/lab/plugins' },
]
