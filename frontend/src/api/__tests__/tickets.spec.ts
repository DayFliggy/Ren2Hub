import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../client'
import { TICKET_UPLOAD_TIMEOUT_MS, ticketsApi } from '../tickets'

const ticket = {
  id: 8,
  title: 'Invoice attachment',
  category: 'billing',
  priority: 'normal',
  status: 'open',
  reply_count: 1,
  message_count: 1,
  last_reply_role: 'user',
  created: 123,
  updated: 123,
}
const message = {
  id: 9,
  role: 'user',
  content: 'Please check',
  images: ['/api/tickets/attachments/7'],
  created: 123,
}
const assignment = { assignee_id: null, assigned_at: 0, updated: 123 }
const adminTicket = {
  ...ticket,
  ...assignment,
  user: { id: 2, username: 'requester', display_name: 'Requester' },
}
afterEach(() => vi.restoreAllMocks())

describe('native ticket API contracts', () => {
  it('loads paginated user/admin tickets and validated message threads', async () => {
    const get = vi
      .spyOn(api, 'get')
      .mockResolvedValueOnce({
        items: [ticket],
        page: 2,
        page_size: 10,
        total: 11,
      })
      .mockResolvedValueOnce({ ticket, messages: [message] })
      .mockResolvedValueOnce({
        items: [adminTicket],
        page: 1,
        page_size: 20,
        total: 1,
      })
      .mockResolvedValueOnce({ ticket: adminTicket, messages: [message] })
    const signal = new AbortController().signal
    expect(await ticketsApi.list({ page: 2, page_size: 10 }, signal)).toEqual({
      items: [ticket],
      page: 2,
      pageSize: 10,
      total: 11,
    })
    expect(get).toHaveBeenLastCalledWith(
      '/api/tickets',
      { page: 2, page_size: 10 },
      { signal }
    )
    expect((await ticketsApi.detail(8, signal)).messages).toEqual([message])
    expect(
      (await ticketsApi.adminList({ page: 1, page_size: 20 })).items
    ).toEqual([adminTicket])
    expect((await ticketsApi.adminDetail(8)).ticket).toEqual(adminTicket)
  })
  it('sends multipart attachments through create and reply endpoints', async () => {
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValueOnce({ ticket })
      .mockResolvedValueOnce({ message })
      .mockResolvedValueOnce({
        message: { ...message, role: 'support' },
        ticket: { ...assignment, status: 'replied' },
      })
    const file = new File(['image'], 'receipt.png', { type: 'image/png' })
    await ticketsApi.create({
      title: ticket.title,
      category: 'billing',
      priority: 'normal',
      content: 'Please check',
      model_id: '',
      request_id: '',
      attachments: [file],
    })
    const body = post.mock.calls[0]![1] as FormData
    expect(post.mock.calls[0]![0]).toBe('/api/tickets')
    expect(body.get('title')).toBe(ticket.title)
    expect(body.getAll('attachments')).toEqual([file])
    await ticketsApi.reply(8, { content: 'Reply', attachments: [file] })
    expect(post.mock.calls[1]![0]).toBe('/api/tickets/8/messages')
    expect(
      (await ticketsApi.adminReply(8, { content: 'Support', attachments: [] }))
        .ticket.status
    ).toBe('replied')
    expect(post.mock.calls[2]![0]).toBe('/api/admin/tickets/8/messages')
    for (const call of post.mock.calls) {
      expect(call[2]).toEqual({ timeoutMs: TICKET_UPLOAD_TIMEOUT_MS })
    }
    expect(TICKET_UPLOAD_TIMEOUT_MS).toBe(120_000)
  })
  it('accepts native partial mutation payloads and null assignment', async () => {
    const patch = vi
      .spyOn(api, 'patch')
      .mockResolvedValueOnce({ ticket: { ...ticket, status: 'closed' } })
      .mockResolvedValueOnce({
        ticket: { id: 8, status: 'closed', updated: 125 },
      })
      .mockResolvedValueOnce({ ticket: { id: 8, ...assignment } })
    expect((await ticketsApi.changeStatus(8, 'closed')).ticket.status).toBe(
      'closed'
    )
    expect(
      (await ticketsApi.adminChangeStatus(8, 'closed')).ticket.updated
    ).toBe(125)
    expect((await ticketsApi.assign(8, null)).ticket.assignee_id).toBeNull()
    expect(patch).toHaveBeenLastCalledWith('/api/admin/tickets/8/assignee', {
      assignee_id: null,
    })
  })
  it('rejects malformed payloads rather than rendering false success', async () => {
    vi.spyOn(api, 'get')
      .mockResolvedValueOnce({
        items: [{ ...ticket, status: 'unknown' }],
        page: 1,
        page_size: 20,
        total: 1,
      })
      .mockResolvedValueOnce({
        ticket,
        messages: [{ ...message, images: null }],
      })
      .mockResolvedValueOnce({ pending: '3', unassigned: 0, mine: 0 })
    await expect(
      ticketsApi.list({ page: 1, page_size: 20 })
    ).rejects.toMatchObject({ code: 'INVALID_RESPONSE' })
    await expect(ticketsApi.detail(8)).rejects.toMatchObject({
      code: 'INVALID_RESPONSE',
    })
    await expect(ticketsApi.summary()).rejects.toMatchObject({
      code: 'INVALID_RESPONSE',
    })
  })
  it('fetches attachments with authentication and rejects foreign or retired URLs', async () => {
    const get = vi.spyOn(api, 'getBlob').mockResolvedValue(new Blob(['image']))
    const signal = new AbortController().signal
    await ticketsApi.attachment('/api/admin/tickets/attachments/7', signal)
    expect(get).toHaveBeenCalledWith('/api/admin/tickets/attachments/7', {
      signal,
    })
    for (const source of [
      'https://outside.test/image',
      '/api/next/tickets/attachments/7',
      '/api/tickets/attachments/../secrets',
    ])
      expect(() => ticketsApi.attachment(source)).toThrow()
    expect(get).toHaveBeenCalledTimes(1)
  })
})
