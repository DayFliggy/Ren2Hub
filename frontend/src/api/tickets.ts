import { api } from './client'
import {
  invalidResponse,
  isRecord,
  parsePage,
  parseStringArray,
  requiredStrictInteger,
  requiredString,
} from './contracts'
import type {
  AdminTicketItem,
  TicketAgent,
  TicketItem,
  TicketMessage,
  TicketQueueSummary,
  TicketStatus,
} from '@/types/console'

export type TicketRow = Omit<TicketItem, 'messages'>
export interface TicketFilters {
  page: number
  page_size: number
  keyword?: string
  status?: string
  category?: string
  priority?: string
  assignee?: string
}
export interface TicketReplyInput {
  content: string
  attachments: File[]
}
export interface TicketCreateInput extends TicketReplyInput {
  title: string
  category: TicketItem['category']
  priority: TicketItem['priority']
  model_id: string
  request_id: string
}

function record(value: unknown, endpoint: string): Record<string, unknown> {
  if (!isRecord(value)) invalidResponse(endpoint)
  return value
}
function choice<const T extends string>(
  value: unknown,
  values: readonly T[],
  endpoint: string
): T {
  if (typeof value !== 'string' || !values.includes(value as T))
    invalidResponse(endpoint)
  return value as T
}
function count(value: unknown, endpoint: string): number {
  const result = requiredStrictInteger(value, endpoint)
  if (result < 0) invalidResponse(endpoint)
  return result
}
function assignment(value: Record<string, unknown>, endpoint: string) {
  return {
    assignee_id:
      value.assignee_id === null ? null : count(value.assignee_id, endpoint),
    assigned_at: count(value.assigned_at, endpoint),
    updated: count(value.updated, endpoint),
  }
}
function status(value: unknown, endpoint: string): TicketStatus {
  return choice(value, ['open', 'replied', 'closed'], endpoint)
}
function parseTicket(value: unknown, endpoint: string): TicketRow {
  const row = record(value, endpoint)
  return {
    id: count(row.id, endpoint),
    title: requiredString(row.title, endpoint),
    category: choice(
      row.category,
      ['billing', 'api', 'model', 'account', 'other'],
      endpoint
    ),
    priority: choice(row.priority, ['low', 'normal', 'high'], endpoint),
    status: status(row.status, endpoint),
    reply_count: count(row.reply_count, endpoint),
    message_count: count(row.message_count, endpoint),
    last_reply_role: choice(row.last_reply_role, ['user', 'support'], endpoint),
    created: count(row.created, endpoint),
    updated: count(row.updated, endpoint),
    ...(row.model_id === undefined
      ? {}
      : { model_id: requiredString(row.model_id, endpoint) }),
    ...(row.request_id === undefined
      ? {}
      : { request_id: requiredString(row.request_id, endpoint) }),
  }
}
function parseAgent(value: unknown, endpoint: string): TicketAgent {
  const row = record(value, endpoint)
  return {
    id: count(row.id, endpoint),
    username: requiredString(row.username, endpoint),
    display_name: requiredString(row.display_name, endpoint),
    ...(row.email === undefined
      ? {}
      : { email: requiredString(row.email, endpoint) }),
  }
}
function parseAdminTicket(value: unknown, endpoint: string): AdminTicketItem {
  const row = record(value, endpoint)
  return {
    ...parseTicket(row, endpoint),
    ...assignment(row, endpoint),
    ...(row.assignee_name === undefined
      ? {}
      : { assignee_name: requiredString(row.assignee_name, endpoint) }),
    user: parseAgent(row.user, endpoint),
  }
}
function parseMessage(value: unknown, endpoint: string): TicketMessage {
  const row = record(value, endpoint)
  return {
    id: count(row.id, endpoint),
    role: choice(row.role, ['user', 'support', 'system'], endpoint),
    content: requiredString(row.content, endpoint),
    images: parseStringArray(row.images, endpoint),
    created: count(row.created, endpoint),
    ...(row.department === undefined
      ? {}
      : {
          department: choice(
            row.department,
            ['support', 'tech', 'finance', 'legal'],
            endpoint
          ),
        }),
  }
}
function parseDetail<T>(
  value: unknown,
  endpoint: string,
  parser: (value: unknown, endpoint: string) => T
) {
  const data = record(value, endpoint)
  if (!Array.isArray(data.messages)) invalidResponse(endpoint)
  return {
    ticket: parser(data.ticket, endpoint),
    messages: data.messages.map((message) => parseMessage(message, endpoint)),
  }
}
function replyBody(input: TicketReplyInput): FormData {
  const body = new FormData()
  body.set('content', input.content)
  for (const file of input.attachments) body.append('attachments', file)
  return body
}

export const ticketsApi = {
  async list(filters: TicketFilters, signal?: AbortSignal) {
    const endpoint = '/api/tickets'
    return parsePage(
      await api.get(endpoint, { ...filters }, { signal }),
      endpoint,
      parseTicket
    )
  },
  async detail(id: number, signal?: AbortSignal) {
    const endpoint = `/api/tickets/${id}`
    return parseDetail(
      await api.get(endpoint, undefined, { signal }),
      endpoint,
      parseTicket
    )
  },
  async create(input: TicketCreateInput) {
    const endpoint = '/api/tickets'
    const body = replyBody(input)
    for (const key of [
      'title',
      'category',
      'priority',
      'model_id',
      'request_id',
    ] as const)
      body.set(key, input[key])
    return {
      ticket: parseTicket(
        record(await api.post(endpoint, body), endpoint).ticket,
        endpoint
      ),
    }
  },
  async reply(id: number, input: TicketReplyInput) {
    const endpoint = `/api/tickets/${id}/messages`
    return {
      message: parseMessage(
        record(await api.post(endpoint, replyBody(input)), endpoint).message,
        endpoint
      ),
    }
  },
  async changeStatus(id: number, next: 'open' | 'closed') {
    const endpoint = `/api/tickets/${id}/status`
    return {
      ticket: parseTicket(
        record(await api.patch(endpoint, { status: next }), endpoint).ticket,
        endpoint
      ),
    }
  },
  async adminList(filters: TicketFilters, signal?: AbortSignal) {
    const endpoint = '/api/admin/tickets'
    return parsePage(
      await api.get(endpoint, { ...filters }, { signal }),
      endpoint,
      parseAdminTicket
    )
  },
  async adminDetail(id: number, signal?: AbortSignal) {
    const endpoint = `/api/admin/tickets/${id}`
    return parseDetail(
      await api.get(endpoint, undefined, { signal }),
      endpoint,
      parseAdminTicket
    )
  },
  async agents(signal?: AbortSignal) {
    const endpoint = '/api/admin/tickets/agents'
    const data = record(
      await api.get(endpoint, undefined, { signal }),
      endpoint
    )
    if (!Array.isArray(data.items)) invalidResponse(endpoint)
    return data.items.map((item) => parseAgent(item, endpoint))
  },
  async summary(signal?: AbortSignal): Promise<TicketQueueSummary> {
    const endpoint = '/api/admin/tickets/summary'
    const data = record(
      await api.get(endpoint, undefined, { signal }),
      endpoint
    )
    return {
      pending: count(data.pending, endpoint),
      unassigned: count(data.unassigned, endpoint),
      mine: count(data.mine, endpoint),
    }
  },
  async adminReply(id: number, input: TicketReplyInput) {
    const endpoint = `/api/admin/tickets/${id}/messages`
    const data = record(await api.post(endpoint, replyBody(input)), endpoint)
    const ticket = record(data.ticket, endpoint)
    return {
      message: parseMessage(data.message, endpoint),
      ticket: {
        ...assignment(ticket, endpoint),
        status: status(ticket.status, endpoint),
      },
    }
  },
  async adminChangeStatus(id: number, next: 'open' | 'closed') {
    const endpoint = `/api/admin/tickets/${id}/status`
    const ticket = record(
      record(await api.patch(endpoint, { status: next }), endpoint).ticket,
      endpoint
    )
    return {
      ticket: {
        id: count(ticket.id, endpoint),
        status: status(ticket.status, endpoint),
        updated: count(ticket.updated, endpoint),
      },
    }
  },
  async assign(id: number, assigneeID: number | null) {
    const endpoint = `/api/admin/tickets/${id}/assignee`
    const ticket = record(
      record(await api.patch(endpoint, { assignee_id: assigneeID }), endpoint)
        .ticket,
      endpoint
    )
    return {
      ticket: {
        id: count(ticket.id, endpoint),
        ...assignment(ticket, endpoint),
      },
    }
  },
  attachment(source: string, signal?: AbortSignal) {
    if (!/^\/api\/(?:admin\/)?tickets\/attachments\/[1-9]\d*$/.test(source))
      invalidResponse('/api/tickets/attachments')
    return api.getBlob(source, { signal })
  },
}
