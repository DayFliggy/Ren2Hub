import { expect, test, type Page } from '@playwright/test'
import { assertNoHorizontalOverflow } from './fixtures'
import { nativeCoreFixture } from './native-core-fixture'

const png = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/l9sAAAAASUVORK5CYII=',
  'base64'
)

async function ticketFixture(page: Page) {
  const ticket = {
    id: 1,
    title: 'Payment <investigation>',
    category: 'billing',
    priority: 'normal',
    status: 'open',
    reply_count: 2,
    message_count: 2,
    last_reply_role: 'support',
    model_id: '',
    request_id: 'request-native',
    created: 1785114000,
    updated: 1785114100,
    assignee_id: null as number | null,
    assignee_name: '',
    assigned_at: 0,
    user: {
      id: 2,
      username: 'requester',
      display_name: 'Native Requester',
      email: 'requester@example.test',
    },
  }
  const messages = [
    {
      id: 1,
      role: 'user',
      content: 'Please check this payment.',
      images: ['/api/tickets/attachments/1'],
      created: 1785114000,
    },
    {
      id: 2,
      role: 'support',
      department: 'support',
      content: 'We are checking the receipt.',
      images: [],
      created: 1785114100,
    },
  ]
  let failReply = true
  let attachmentReads = 0
  const writes: Array<{ method: string; path: string; body: unknown }> = []
  await page.route(
    /\/api\/(?:admin\/)?tickets(?:\/[^?]*)?(?:\?.*)?$/,
    async (route) => {
      const req = route.request(),
        path = new URL(req.url()).pathname,
        method = req.method()
      const ok = (data: unknown) =>
        route.fulfill({ json: { success: true, data } })
      if (
        method === 'GET' &&
        /^\/api\/(?:admin\/)?tickets\/attachments\/1$/.test(path)
      ) {
        expect(method).toBe('GET')
        expect(req.headers().authorization).toBe('Bearer fixture-access')
        attachmentReads++
        return route.fulfill({ contentType: 'image/png', body: png })
      }
      if (path === '/api/admin/tickets/summary')
        return ok({
          pending: ticket.status === 'closed' ? 0 : 1,
          unassigned: ticket.assignee_id ? 0 : 1,
          mine: ticket.assignee_id === 1 ? 1 : 0,
        })
      if (path === '/api/admin/tickets/agents')
        return ok({
          items: [
            { id: 1, username: 'native.root', display_name: 'Native Root' },
          ],
        })
      if (
        method === 'GET' &&
        (path === '/api/tickets' || path === '/api/admin/tickets')
      )
        return ok({ items: [ticket], total: 1, page: 1, page_size: 10 })
      if (
        method === 'GET' &&
        (path === '/api/tickets/1' || path === '/api/admin/tickets/1')
      )
        return ok({
          ticket,
          messages: messages.map((message) => ({
            ...message,
            images: message.images.map((url) =>
              path.startsWith('/api/admin/')
                ? url.replace('/api/tickets/', '/api/admin/tickets/')
                : url
            ),
          })),
        })
      if (method === 'POST' && path.endsWith('/1/messages')) {
        const form = await new Response(req.postDataBuffer(), {
          headers: { 'Content-Type': req.headers()['content-type']! },
        }).formData()
        const content = String(form.get('content'))
        const attachments = form.getAll('attachments') as File[]
        writes.push({
          method,
          path,
          body: {
            content,
            attachments: attachments.map((file) => ({
              name: file.name,
              type: file.type,
              size: file.size,
            })),
          },
        })
        if (failReply)
          return route.fulfill({
            json: { success: false, message: 'Reply unavailable' },
          })
        const admin = path.startsWith('/api/admin/')
        const message = {
          id: 3,
          role: admin ? 'support' : 'user',
          content,
          images: [],
          created: 1785114200,
        }
        messages.push(message)
        ticket.status = admin ? 'replied' : 'open'
        ticket.updated = message.created
        return ok({ message, ticket })
      }
      if (method === 'PATCH' && path.endsWith('/1/status')) {
        const body = req.postDataJSON()
        writes.push({ method, path, body })
        expect(['open', 'closed']).toContain(body.status)
        ticket.status = body.status
        return ok({ ticket })
      }
      if (method === 'PATCH' && path === '/api/admin/tickets/1/assignee') {
        const body = req.postDataJSON()
        writes.push({ method, path, body })
        expect(body).toEqual({ assignee_id: 1 })
        ticket.assignee_id = 1
        ticket.assignee_name = 'Native Root'
        ticket.assigned_at = 1785114200
        return ok({ ticket })
      }
      return route.fallback()
    }
  )
  return {
    writes,
    allowReply: () => {
      failReply = false
    },
    attachmentReads: () => attachmentReads,
  }
}

for (const theme of ['light', 'dark'] as const) {
  for (const mobile of [false, true]) {
    test(`native personal ticket reply, attachment and status ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 }
      )
      const base = await nativeCoreFixture(page, theme)
      const fixture = await ticketFixture(page)
      await page.goto('/tickets')
      await page
        .getByRole('button', { name: 'Payment <investigation>', exact: true })
        .click()
      await expect(page).toHaveURL(/\/tickets\/1$/)
      await expect(page.getByText('Support', { exact: true })).toBeVisible()
      await expect.poll(fixture.attachmentReads).toBe(1)
      await expect(page.locator('img[src^="blob:"]')).toBeVisible()
      const reply = page.getByRole('textbox', { name: 'Type your reply…' })
      await reply.fill('Receipt <literal> & detail')
      await page
        .getByRole('button', { name: 'Attachments', exact: true })
        .click()
      await page.locator('input[type=file]').setInputFiles({
        name: 'receipt.png',
        mimeType: 'image/png',
        buffer: png,
      })
      await page.getByRole('button', { name: 'Send', exact: true }).click()
      await expect(
        page.getByText('Reply unavailable', { exact: true })
      ).toBeVisible()
      await expect(reply).toHaveValue('Receipt <literal> & detail')
      fixture.allowReply()
      await page.getByRole('button', { name: 'Send', exact: true }).click()
      await expect(reply).toHaveValue('')
      await expect(
        page.getByText('Receipt <literal> & detail', { exact: true })
      ).toBeVisible()
      expect(fixture.writes.slice(0, 2)).toEqual(
        Array.from({ length: 2 }, () => ({
          method: 'POST',
          path: '/api/tickets/1/messages',
          body: {
            content: 'Receipt <literal> & detail',
            attachments: [
              { name: 'receipt.png', type: 'image/png', size: png.length },
            ],
          },
        }))
      )
      await page
        .getByRole('button', { name: 'Close ticket', exact: true })
        .click()
      await page
        .getByRole('dialog')
        .getByRole('button', { name: 'Close ticket', exact: true })
        .click()
      await expect(
        page.getByText('This ticket is closed', { exact: true })
      ).toBeVisible()
      await expect(reply).toHaveCount(0)
      await page.getByRole('button', { name: 'Reopen', exact: true }).click()
      await expect(reply).toBeVisible()
      expect(fixture.writes.slice(2).map((write) => write.body)).toEqual([
        { status: 'closed' },
        { status: 'open' },
      ])
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('native-ticket-personal.png'),
        fullPage: true,
      })
      expect(base.unexpected).toEqual([])
      expect(base.errors).toEqual([])
    })

    test(`native admin ticket assignment and reply ${theme} ${mobile ? 'mobile' : 'desktop'}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize(
        mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 }
      )
      const base = await nativeCoreFixture(page, theme)
      const fixture = await ticketFixture(page)
      fixture.allowReply()
      await page.goto('/ticket-management/1')
      const workspace = page.getByLabel('Ticket workspace', { exact: true })
      await expect(
        workspace.getByRole('heading', { name: 'Payment <investigation>' })
      ).toBeVisible()
      await expect(
        workspace.getByText('Native Requester', { exact: true })
      ).toBeVisible()
      await workspace.getByRole('button', { name: 'Take', exact: true }).click()
      await expect(
        page.getByText('Assignee updated', { exact: true })
      ).toBeVisible()
      expect(fixture.writes[0]).toEqual({
        method: 'PATCH',
        path: '/api/admin/tickets/1/assignee',
        body: { assignee_id: 1 },
      })
      await workspace
        .getByRole('textbox', { name: 'Type your reply…' })
        .fill('Payment verified locally.')
      await workspace.getByRole('button', { name: 'Send', exact: true }).click()
      await expect(
        workspace.getByText('Payment verified locally.', { exact: true })
      ).toBeVisible()
      expect(fixture.writes[1]).toEqual({
        method: 'POST',
        path: '/api/admin/tickets/1/messages',
        body: { content: 'Payment verified locally.', attachments: [] },
      })
      await assertNoHorizontalOverflow(page)
      await page.screenshot({
        path: testInfo.outputPath('native-ticket-admin.png'),
        fullPage: true,
      })
      expect(base.unexpected).toEqual([])
      expect(base.errors).toEqual([])
    })
  }
}
