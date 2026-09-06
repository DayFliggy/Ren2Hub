// Compatibility only. Canonical paths and access rules live in the route records.
const aliases: Record<string, string> = {
  '/console': '/dashboard',
  '/auth/sign-in': '/sign-in',
  '/login': '/sign-in',
  '/auth/sign-up': '/sign-up',
  '/register': '/sign-up',
  '/auth/reset': '/forgot-password',
  '/user/reset': '/reset',
  '/forbidden': '/403',
  '/token': '/keys',
  '/channel': '/channels',
  '/user': '/users',
  '/personal': '/profile',
  '/topup': '/wallet',
  '/redemption': '/redemption-codes',
  '/log': '/usage-logs',
  '/logs': '/usage-logs',
  '/logs/drawing': '/usage-logs/drawing',
  '/logs/tasks': '/usage-logs/task',
  '/logs/operations': '/usage-logs/operations',
  '/usage-logs/common': '/usage-logs',
  '/usage-logs/consume': '/usage-logs',
  '/usage-logs/tasks': '/usage-logs/task',
  '/midjourney': '/usage-logs/drawing',
  '/task': '/usage-logs/task',
  '/admin/models': '/models/metadata',
  '/models/models': '/models/metadata',
  '/admin/vendors': '/models/vendors',
  '/admin/prefill-groups': '/models/prefill-groups',
  '/admin/system-info': '/system-info',
}

const settingsTabs: Record<string, string> = {
  operation: 'operations/behavior',
  dashboard: 'content/console-content',
  chats: 'content/chat',
  drawing: 'content/drawing',
  payment: 'billing/payment',
  ratio: 'billing/pricing',
  ratelimit: 'security/rate-limit',
  models: 'models/routing',
  'model-deployment': 'models/auto-pricing',
  performance: 'operations/performance',
  system: 'site/system-info',
  other: 'site/system-info',
}

const settingsSections: Record<string, string> = {
  'billing/currency': 'billing/quota',
  'billing/model-pricing': 'billing/pricing',
  'billing/group-pricing': 'billing/pricing',
  'models/global': 'models/routing',
  'models/routing-reliability': 'models/routing',
  'models/gemini': 'models/vendor',
  'models/claude': 'models/vendor',
  'models/grok': 'models/vendor',
  'models/channel-affinity': 'models/affinity',
  'models/model-deployment': 'models/auto-pricing',
  'models/deployment': 'models/auto-pricing',
  'site/header-navigation': 'site/navigation',
  'site/sidebar-modules': 'site/navigation',
  'content/dashboard': 'content/console-content',
  'content/announcements': 'content/console-content',
  'content/api-info': 'content/console-content',
  'content/faq': 'content/console-content',
  'content/uptime-kuma': 'content/console-content',
  'operations/alerts': 'operations/monitoring',
  'operations/logs': 'operations/maintenance',
  'operations/update-checker': 'operations/maintenance',
  'security/sensitive-words': 'security/sensitive',
}

export function canonicalLegacyPath(
  path: string,
  query: URLSearchParams
): string {
  path = path.replace(/^\/next(?=\/|$)/, '') || '/'
  if (path.startsWith('//') || path.includes('\\')) return '/404'
  path = path.replace(/^\/console\//, '/').replace(/\/+$/, '') || '/'
  path = aliases[path] ?? path
  if (path === '/setting') {
    path = `/system-settings/${settingsTabs[query.get('tab') ?? ''] ?? 'site/system-info'}`
  }
  if (path === '/usage-logs') {
    const tab = query.get('tab')
    if (tab === 'drawing' || tab === 'midjourney') path += '/drawing'
    if (tab === 'task' || tab === 'tasks') path += '/task'
  }
  const section = path.replace(/^\/system-settings\//, '')
  if (section !== path && settingsSections[section]) {
    path = `/system-settings/${settingsSections[section]}`
  }
  if (
    /^\/(playground|chat|chat2link|chat-presets)(\/|$)/.test(path) ||
    /^\/(models\/deployments|deployment|admin\/deployments)(\/|$)/.test(path) ||
    /^\/system-settings\/content\/(chat|chats|chat-presets)(\/|$)/.test(path)
  )
    return '/404'
  return path
}
