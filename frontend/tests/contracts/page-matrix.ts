import { SYSTEM_SETTINGS_DOMAINS } from '../../src/constants/systemSettingsCatalog'

export interface PageCase {
  path: string
  view: string
  guest?: boolean
  setup?: boolean
  error?: boolean
}

// Canonical routes, including dynamic detail routes and every settings section.
// The expected SFC prevents an unexpected permission redirect passing as a page.
export const pages: PageCase[] = [
  { path: '/', view: 'HomeView' },
  { path: '/pricing', view: 'PricingView' },
  { path: '/pricing/gpt-native', view: 'PricingView' },
  { path: '/rankings', view: 'RankingsView' },
  ...['about', 'privacy-policy', 'user-agreement'].map((path) => ({
    path: `/${path}`,
    view: 'PublicDocumentView',
  })),
  ...[401, 403, 404, 500, 503].map((code) => ({
    path: `/${code}`,
    view: 'StatusErrorView',
  })),
  { path: '/missing-page', view: 'NotFoundView' },
  { path: '/sign-in', view: 'SignInView', guest: true },
  { path: '/sign-up', view: 'SignUpView', guest: true },
  { path: '/forgot-password', view: 'ResetPasswordView', guest: true },
  {
    path: '/reset?email=member%40example.test&token=local-fixture',
    view: 'ResetPasswordConfirmView',
    guest: true,
  },
  { path: '/oauth/github', view: 'OAuthCallbackView', guest: true },
  { path: '/setup', view: 'SetupView', guest: true, setup: true },
  { path: '/setup/error', view: 'SetupErrorView', guest: true },
  { path: '/dashboard', view: 'DashboardView' },
  { path: '/ops', view: 'OpsMonitorView' },
  { path: '/activity', view: 'ActivityView' },
  { path: '/models', view: 'ModelsView' },
  { path: '/keys', view: 'KeysView' },
  { path: '/keys/7/routing', view: 'TokenRoutingView' },
  { path: '/usage-logs', view: 'LogsView' },
  { path: '/usage-logs/drawing', view: 'DrawingLogsView' },
  { path: '/usage-logs/task', view: 'TaskLogsView' },
  { path: '/usage-logs/audit', view: 'AuditLogsView' },
  { path: '/channels', view: 'ChannelsView' },
  { path: '/ticket-management', view: 'AdminTicketsView' },
  { path: '/ticket-management/1', view: 'AdminTicketsView' },
  { path: '/users', view: 'UsersView' },
  { path: '/redemption-codes', view: 'RedemptionView' },
  { path: '/orders', view: 'OrdersView' },
  { path: '/tickets', view: 'TicketsView' },
  { path: '/tickets/1', view: 'TicketDetailView' },
  { path: '/wallet', view: 'WalletView' },
  { path: '/invite', view: 'InviteView' },
  { path: '/settings', view: 'AccountSettingsView' },
  { path: '/security', view: 'SecurityAccessView' },
  { path: '/profile', view: 'AccountCenterView' },
  { path: '/task-plugins', view: 'TaskPluginsView' },
  ...['metadata', 'vendors', 'pricing'].map((section) => ({
    path: `/models/${section}`,
    view: 'ModelManagementView',
  })),
  ...SYSTEM_SETTINGS_DOMAINS.flatMap((domain) =>
    domain.sections.map((section) => ({
      path: `/system-settings/${domain.id}/${section.id}`,
      view: 'SystemSettingsView',
    }))
  ),
]

export const viewports = {
  mobile: { width: 390, height: 844 },
  tablet: { width: 1024, height: 820 },
  desktop: { width: 1440, height: 900 },
} as const
