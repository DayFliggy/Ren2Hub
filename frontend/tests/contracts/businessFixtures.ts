import type { Page } from '@playwright/test'
import { configureStablePage, type VisualTheme } from './fixtures'

export async function configureBusinessPage(page: Page, theme: VisualTheme) {
  await configureStablePage(page, { theme, authenticated: true })
  const now = Date.parse('2026-07-27T04:00:00Z') / 1000
  const user = {
    id: 1,
    username: 'visual.root',
    display_name: 'Visual Root',
    email: 'visual@example.test',
    role: 100,
    status: 1,
    quota: 500000,
    used_quota: 100000,
    request_count: 5,
    setting: JSON.stringify({ language: 'zh-CN' }),
  }
  await page.route('**/api/user/self', (route) =>
    route.fulfill({ json: { success: true, data: user } })
  )
  await page.route('**/api/user/auth/refresh*', (route) =>
    route.fulfill({
      json: {
        success: true,
        data: {
          access_token: 'visual-access-token',
          token_type: 'Bearer',
          access_expires_at: now + 900,
          session: {
            sid: 'visual-session',
            current: true,
            login_method: 'password',
            ip: '127.0.0.1',
            user_agent: 'Playwright',
            created_at: now - 3600,
            last_active_at: now,
            expires_at: now + 86400,
          },
          user,
        },
      },
    })
  )
  await page.route('**/api/admin/tickets/summary', (route) =>
    route.fulfill({
      json: { success: true, data: { pending: 2, unassigned: 1, mine: 1 } },
    })
  )
  return user
}

export async function configureNativeTickets(
  page: Page,
  theme: VisualTheme,
  { userDeleted = false } = {}
) {
  await configureBusinessPage(page, theme)
  const ticket = {
    id: 1,
    title: 'Production request investigation',
    category: 'api',
    priority: 'normal',
    status: 'replied',
    reply_count: 2,
    message_count: 2,
    last_reply_role: 'support',
    model_id: 'gpt-4.1',
    request_id: 'req_visual_2026',
    created: 1784952000,
    updated: 1785038400,
  }
  const messages = [
    {
      id: 1,
      role: 'user',
      content: 'Please help inspect this request.',
      images: [],
      created: ticket.created,
    },
    {
      id: 2,
      role: 'support',
      department: 'support',
      content: 'The request has been reviewed.',
      images: [],
      created: ticket.updated,
    },
  ]
  const adminTicket = {
    ...ticket,
    assignee_id: null,
    assigned_at: 0,
    user: {
      id: 8,
      username: 'visual.requester',
      display_name: 'Visual Requester',
    },
    user_deleted: userDeleted,
  }
  const responses: Record<string, unknown> = {
    '/api/tickets': { items: [ticket], total: 1, page: 1, page_size: 10 },
    '/api/tickets/1': { ticket, messages },
    '/api/admin/tickets': {
      items: [adminTicket],
      total: 1,
      page: 1,
      page_size: 20,
    },
    '/api/admin/tickets/1': { ticket: adminTicket, messages },
    '/api/admin/tickets/agents': {
      items: [{ id: 1, username: 'visual.root', display_name: 'Visual Root' }],
    },
  }
  await page.route(/\/api\/(?:admin\/)?tickets(?:\/|\?|$)/, (route) => {
    const data = responses[new URL(route.request().url()).pathname]
    return data === undefined
      ? route.fallback()
      : route.fulfill({ json: { success: true, data } })
  })
}
