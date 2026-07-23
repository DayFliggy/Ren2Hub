import { ApiError, type ApiResponse } from '../types'
import type { HttpMethod, RequestOptions } from '../transport'
import { readDemoUser, writeDemoUser } from '../demoStorage'
import type { UserInfo } from '@/types/auth'
import { MODELS, marketSources } from '@/constants/console'
import type { PrizeRecord } from '@/types/bigame'
import type {
  ListingStatus,
  LogType,
  MarketListing,
  MarketModelType,
  TicketCategory,
  TicketItem,
  TicketMessage,
  TicketPriority,
  TicketStatus,
  TokenChannel,
  TokenItem,
  TokenSummary,
  TokenType,
} from '@/types/console'
import type { LeaderPeriod } from '@/types/farm'
import type { CommunityCategory } from '@/types/lab'
import {
  GROUPS,
  PLATFORM_CHANNEL_NAME,
  activities,
  activitySummary,
  addInvoice,
  addMyChannel,
  currentSubscription,
  dashboardStats,
  flowSeries,
  inviteInfo,
  invoices,
  logs,
  marketListings,
  marketMerchants,
  marketModels,
  marketVendors,
  marketplaceChannels,
  modelVendorMap,
  mockUser,
  modelShare,
  myChannels,
  plans,
  tickets,
  tokens,
  topupRecords,
} from './data'
import {
  assetItems,
  assetStorage,
  chatConversations,
  communityWorks,
  installedPlugins,
  labModelPicks,
  labStarters,
  marketPlugins,
  mcpServers,
  noteItems,
  skillItems,
  studioGallery,
  studioTools,
} from './lab'
import {
  farmState,
  farmPlots,
  ranchAnimals,
  fishingState,
  myPet,
  mineState,
  leaderboard,
  rebateTiers,
  rebateState,
} from './farm'
import {
  gameWallet,
  milestones,
  spinPrizes,
  blindBoxPrizes,
  prizeRecords,
} from './bigame'
import { getMockDelay, mockRuntime } from './state'

type Ctx = RequestOptions & { headers: Record<string, string> }

function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) {
      reject(new DOMException('The request was aborted', 'AbortError'))
      return
    }

    const onAbort = () => {
      clearTimeout(timer)
      reject(new DOMException('The request was aborted', 'AbortError'))
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve()
    }, ms)

    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

function ok<T>(data: T, message = ''): ApiResponse<T> {
  return { success: true, message, data }
}

function fail<T = never>(message: string): ApiResponse<T> {
  return { success: false, message, data: undefined as never }
}

function requireAuth(ctx: Ctx) {
  const uid = Number(ctx.headers['X-Ren2Hub-Demo-User'])
  const stored = readDemoUser()
  if (!stored || uid !== stored.id) {
    throw new ApiError('登录状态已失效，请重新登录', { status: 401 })
  }
}

function paginate<T>(items: T[], params: Record<string, unknown>) {
  const page = Math.max(1, Number(params.page ?? 1))
  const pageSize = Math.min(100, Math.max(1, Number(params.page_size ?? 10)))
  const start = (page - 1) * pageSize
  return {
    items: items.slice(start, start + pageSize),
    total: items.length,
    page,
    pageSize,
  }
}

/**
 * Auto tokens don't own a channel list — the router picks the best channels
 * across the platform pool and the user's added market channels at request
 * time. Here we approximate "best" by scoring each candidate deterministically
 * and returning the top slice, so the UI can show what the router would pick.
 */
function computeAutoChannels(): TokenChannel[] {
  const platform = marketSources.map((name, i) => ({
    name,
    // Stable pseudo-score: earlier sources are the primary official routes.
    score: 100 - i * 2,
  }))
  const market = myChannels
    .filter((c) => c.status === 'active')
    .map((c) => {
      const listing = marketListings.find((l) => l.id === c.listingId)
      return {
        name: c.merchantName,
        score: listing
          ? listing.availability * 0.6 + listing.qcScore * 0.4
          : 60,
      }
    })
  const seen = new Set<string>()
  return [...platform, ...market]
    .sort((a, b) => b.score - a.score)
    .filter((c) => !seen.has(c.name) && seen.add(c.name))
    .slice(0, 8)
    .map((c) => ({ name: c.name, enabled: true }))
}

/** Auto tokens carry computed channels in every response. */
function withComputedChannels(item: TokenItem): TokenItem {
  return item.type === 'auto'
    ? { ...item, channels: computeAutoChannels() }
    : item
}

function toTokenSummary(item: TokenItem): TokenSummary {
  const { key, ...summary } = withComputedChannels(item)
  return {
    ...summary,
    key_preview: `${key.slice(0, 7)}...${key.slice(-4)}`,
  }
}

export async function dispatchMock<T>(
  method: HttpMethod,
  url: string,
  ctx: Ctx
): Promise<ApiResponse<T>> {
  await sleep(getMockDelay(), ctx.signal)
  const path = url.split('?')[0]
  const params = ctx.params ?? {}
  const body = (ctx.data ?? {}) as Record<string, unknown>

  /* ---------------- public ---------------- */
  if (path === '/api/status' && method === 'GET') {
    return ok({
      version: 'v2.6.1',
      system_name: 'Ren2Hub',
      logo: './favicon.svg',
      register_enabled: true,
      uptime_kuma_enabled: true,
    }) as ApiResponse<T>
  }
  if (path === '/api/notice' && method === 'GET') {
    return ok(
      'gpt-image-2 图像接口已上线；全线模型价格下调，透明计费。'
    ) as ApiResponse<T>
  }

  /* ---------------- auth ---------------- */
  if (path === '/api/user/login' && method === 'POST') {
    const username = String(body.username ?? '').trim()
    const password = String(body.password ?? '')
    if (!username || password.length < 6) {
      return fail('用户名或密码错误') as ApiResponse<T>
    }
    const user = { ...mockUser, username, display_name: mockUser.display_name }
    return ok({ user, message: '登录成功' }) as ApiResponse<T>
  }
  if (path === '/api/user/register' && method === 'POST') {
    const username = String(body.username ?? '').trim()
    const email = String(body.email ?? '').trim()
    const password = String(body.password ?? '')
    if (!username || !/^\S+@\S+\.\S+$/.test(email) || password.length < 8) {
      return fail('注册信息不完整或密码不足 8 位') as ApiResponse<T>
    }
    return ok({ message: '注册成功，请查收验证邮件' }) as ApiResponse<T>
  }
  if (path === '/api/user/reset' && method === 'POST') {
    const email = String(body.email ?? '').trim()
    if (!/^\S+@\S+\.\S+$/.test(email))
      return fail('邮箱格式不正确') as ApiResponse<T>
    return ok({ message: '重置链接已发送至邮箱' }) as ApiResponse<T>
  }
  if (path === '/api/user/logout' && method === 'POST') {
    requireAuth(ctx)
    return ok({ message: '已退出登录' }) as ApiResponse<T>
  }

  /* ---------------- protected: everything below needs auth ---------------- */
  requireAuth(ctx)

  if (path === '/api/user/self' && method === 'GET') {
    const stored = readDemoUser()
    return ok(stored) as ApiResponse<T>
  }
  if (path === '/api/user/self' && method === 'PUT') {
    const stored = readDemoUser()!
    const merged = { ...stored, ...body } as UserInfo
    writeDemoUser(merged)
    return ok({ user: merged, message: '资料已更新' }) as ApiResponse<T>
  }
  if (path === '/api/user/self/password' && method === 'PUT') {
    if (String(body.new_password ?? '').length < 8) {
      return fail('新密码至少 8 位') as ApiResponse<T>
    }
    return ok({ message: '密码已更新' }) as ApiResponse<T>
  }

  /* ---------------- dashboard & logs ---------------- */
  if (path === '/api/data/self' && method === 'GET') {
    return ok({ ...dashboardStats, model_share: modelShare }) as ApiResponse<T>
  }
  if (path === '/api/data/flow/self' && method === 'GET') {
    return ok(flowSeries) as ApiResponse<T>
  }
  if (path === '/api/log/self' && method === 'GET') {
    const type = String(params.type ?? '') as LogType | ''
    const keyword = String(params.keyword ?? '').toLowerCase()
    const start = Number(params.start ?? 0)
    const end = Number(params.end ?? 0)
    const filtered = logs.filter((l) => {
      if (type && l.type !== type) return false
      if (keyword && !l.model.toLowerCase().includes(keyword)) return false
      if (start && l.created < start) return false
      if (end && l.created > end) return false
      return true
    })
    return ok(paginate(filtered, params)) as ApiResponse<T>
  }
  if (path === '/api/log/self/stat' && method === 'GET') {
    return ok({
      total_requests: dashboardStats.total_requests,
      total_quota: dashboardStats.used_quota,
      today_requests: dashboardStats.today_requests,
      today_quota: dashboardStats.today_quota,
    }) as ApiResponse<T>
  }

  /* ---------------- tokens (API keys) ---------------- */
  if (path === '/api/token/' && method === 'GET') {
    const keyword = String(params.keyword ?? '').toLowerCase()
    const filtered = tokens
      .filter((t) => (keyword ? t.name.toLowerCase().includes(keyword) : true))
      .map(toTokenSummary)
    return ok(paginate(filtered, params)) as ApiResponse<T>
  }
  if (path === '/api/token/' && method === 'POST') {
    const name = String(body.name ?? '').trim()
    if (!name) return fail('令牌名称不能为空') as ApiResponse<T>
    const type = String(body.type ?? 'platform') as TokenType
    if (!['platform', 'auto', 'market'].includes(type)) {
      return fail('无效的令牌类型') as ApiResponse<T>
    }
    // Custom key: optional; must be unique and follow the sk- prefix format.
    const customKey = String(body.key ?? '').trim()
    if (customKey) {
      if (!/^sk-[A-Za-z0-9_-]{8,64}$/.test(customKey)) {
        return fail(
          '自定义密钥需以 sk- 开头，8-64 位字母数字'
        ) as ApiResponse<T>
      }
      if (tokens.some((t) => t.key === customKey)) {
        return fail('该密钥已存在，请更换') as ApiResponse<T>
      }
    }
    const item: TokenItem = {
      id: mockRuntime.nextTokenId++,
      name,
      key:
        customKey ||
        `sk-${Math.random().toString(36).slice(2)}${Math.random().toString(36).slice(2)}`.slice(
          0,
          43
        ),
      type,
      status: 1,
      used_quota: 0,
      remain_quota: Number(body.remain_quota ?? 0),
      unlimited: Boolean(body.unlimited),
      group: String(body.group ?? 'default'),
      model_limits: (body.model_limits as string[]) ?? [],
      ip_limits: (body.ip_limits as string[]) ?? [],
      rate_limit: Math.max(0, Number(body.rate_limit ?? 0)),
      max_ratio:
        type === 'platform'
          ? undefined
          : Number(body.max_ratio ?? 0) || undefined,
      load_balance: Boolean(body.load_balance),
      channels:
        type === 'auto' ? [] : ((body.channels as TokenChannel[]) ?? []),
      expired_time: Number(body.expired_time ?? -1),
      created_time: Math.floor(Date.now() / 1000),
    }
    tokens.unshift(item)
    return ok({
      item: toTokenSummary(item),
      message: '令牌已创建',
    }) as ApiResponse<T>
  }
  const tokenIdMatch = path.match(/^\/api\/token\/(\d+)(\/key)?$/)
  if (tokenIdMatch) {
    const id = Number(tokenIdMatch[1])
    const item = tokens.find((t) => t.id === id)
    if (!item) return fail('令牌不存在') as ApiResponse<T>

    if (tokenIdMatch[2] === '/key' && method === 'GET') {
      // Full-key read: rate-limited + cache-disabled on the real backend.
      return ok({ key: item.key }) as ApiResponse<T>
    }
    if (method === 'PUT') {
      if (body.channels !== undefined && item.type === 'auto') {
        return fail('自动令牌的渠道由系统计算，不可编辑') as ApiResponse<T>
      }
      Object.assign(item, {
        name: body.name !== undefined ? String(body.name) : item.name,
        status:
          body.status !== undefined ? (body.status as 1 | 2) : item.status,
        remain_quota:
          body.remain_quota !== undefined
            ? Number(body.remain_quota)
            : item.remain_quota,
        unlimited:
          body.unlimited !== undefined
            ? Boolean(body.unlimited)
            : item.unlimited,
        group: body.group !== undefined ? String(body.group) : item.group,
        model_limits: (body.model_limits as string[]) ?? item.model_limits,
        ip_limits: (body.ip_limits as string[]) ?? item.ip_limits,
        rate_limit:
          body.rate_limit !== undefined
            ? Math.max(0, Number(body.rate_limit))
            : item.rate_limit,
        max_ratio:
          item.type === 'platform'
            ? undefined
            : body.max_ratio !== undefined
              ? Number(body.max_ratio) || undefined
              : item.max_ratio,
        load_balance:
          body.load_balance !== undefined
            ? Boolean(body.load_balance)
            : item.load_balance,
        channels: (body.channels as TokenChannel[]) ?? item.channels,
        expired_time:
          body.expired_time !== undefined
            ? Number(body.expired_time)
            : item.expired_time,
      })
      return ok({
        item: toTokenSummary(item),
        message: '令牌已更新',
      }) as ApiResponse<T>
    }
    if (method === 'DELETE') {
      tokens.splice(tokens.indexOf(item), 1)
      return ok({ message: '令牌已删除' }) as ApiResponse<T>
    }
  }
  if (path === '/api/token/batch' && method === 'POST') {
    const ids = (body.ids as number[]) ?? []
    ids.forEach((id) => {
      const idx = tokens.findIndex((t) => t.id === id)
      if (idx >= 0) tokens.splice(idx, 1)
    })
    return ok({ message: `已删除 ${ids.length} 个令牌` }) as ApiResponse<T>
  }

  /* ---------------- wallet & topup ---------------- */
  if (path === '/api/user/topup/records' && method === 'GET') {
    return ok(paginate(topupRecords, params)) as ApiResponse<T>
  }
  if (path === '/api/user/topup/redeem/records' && method === 'GET') {
    const redeemOnly = topupRecords.filter((r) => r.method === 'redeem')
    return ok(paginate(redeemOnly, params)) as ApiResponse<T>
  }
  if (path === '/api/user/topup' && method === 'POST') {
    const amount = Number(body.amount ?? 0)
    if (!amount || amount < 1) return fail('充值金额至少 $1') as ApiResponse<T>
    // Payment confirmation is server-callback driven on the real backend;
    // here we simulate a pending order that the records list later confirms.
    topupRecords.unshift({
      id: 1000 + topupRecords.length,
      trade_no: `T${Date.now()}`,
      amount,
      money: amount * 500_000,
      method: String(body.method ?? 'epay') as 'epay' | 'stripe' | 'creem',
      status: 'pending',
      created: Math.floor(Date.now() / 1000),
    })
    return ok({
      message: '支付单已创建，到账以服务端回调为准',
      trade_no: topupRecords[0].trade_no,
    }) as ApiResponse<T>
  }
  if (path === '/api/user/topup/redeem' && method === 'POST') {
    const code = String(body.code ?? '').trim()
    if (code.length < 8) return fail('兑换码无效或已被使用') as ApiResponse<T>
    topupRecords.unshift({
      id: 1000 + topupRecords.length,
      trade_no: `R${Date.now()}`,
      amount: 10,
      money: 5_000_000,
      method: 'redeem',
      status: 'success',
      created: Math.floor(Date.now() / 1000),
    })
    return ok({
      message: '兑换成功，$10 已入账',
      quota: 5_000_000,
    }) as ApiResponse<T>
  }

  /* ---------------- subscription ---------------- */
  if (path === '/api/subscription/plans' && method === 'GET') {
    return ok(plans) as ApiResponse<T>
  }
  if (path === '/api/subscription/self' && method === 'GET') {
    return ok({ ...currentSubscription }) as ApiResponse<T>
  }
  if (path === '/api/subscription/self' && method === 'PUT') {
    if (body.auto_renew !== undefined)
      currentSubscription.auto_renew = Boolean(body.auto_renew)
    return ok({
      ...currentSubscription,
      message: '订阅设置已更新',
    }) as ApiResponse<T>
  }
  if (path === '/api/subscription/purchase' && method === 'POST') {
    const plan = plans.find((p) => p.id === Number(body.plan_id))
    if (!plan) return fail('套餐不存在') as ApiResponse<T>
    return ok({
      message: `已创建「${plan.name}」支付单，到账以回调为准`,
    }) as ApiResponse<T>
  }

  /* ---------------- invite & rebate ---------------- */
  if (path === '/api/invite/self' && method === 'GET') {
    // Return a fresh object each call (like a real JSON response) so the
    // caller's `ref` sees a new reference after a mutating action (transfer)
    // and reliably re-renders — returning the singleton would be a no-op set.
    return ok({ ...inviteInfo }) as ApiResponse<T>
  }
  if (path === '/api/invite/transfer' && method === 'POST') {
    const amount = Number(body.amount ?? 0)
    if (amount <= 0 || amount > inviteInfo.transferable) {
      return fail('转出额度超出可转余额') as ApiResponse<T>
    }
    inviteInfo.transferable -= amount
    return ok({ message: '已转入账户余额' }) as ApiResponse<T>
  }

  /* ---------------- meta ---------------- */
  if (path === '/api/models/available' && method === 'GET') {
    return ok({ models: MODELS, groups: GROUPS }) as ApiResponse<T>
  }

  /* ---------------- model plaza ---------------- */
  if (path === '/api/models/market' && method === 'GET') {
    requireAuth(ctx)
    // Channels are linked to the marketplace: every model routes through the
    // platform, plus any added (active) market channel whose merchant supports it.
    const activeMine = myChannels.filter((c) => c.status === 'active')
    const models = marketModels.map((m) => {
      const merchantNames = [
        ...new Set(
          activeMine
            .filter((c) => c.supportedModels.includes(m.name))
            .map((c) => c.merchantName)
        ),
      ]
      return { ...m, channels: [PLATFORM_CHANNEL_NAME, ...merchantNames] }
    })
    const channels = [
      PLATFORM_CHANNEL_NAME,
      ...new Set(activeMine.map((c) => c.merchantName)),
    ]
    return ok({
      models,
      channels,
      vendors: marketVendors,
    }) as ApiResponse<T>
  }

  /* ---------------- tickets (support ledger) ---------------- */
  if (path === '/api/ticket/' && method === 'GET') {
    const status = String(params.status ?? '') as TicketStatus | ''
    const keyword = String(params.keyword ?? '').toLowerCase()
    const filtered = tickets
      .filter((t) => {
        if (status && t.status !== status) return false
        if (keyword && !t.title.toLowerCase().includes(keyword)) return false
        return true
      })
      .sort((a, b) => b.updated - a.updated)
      // The list must not carry each ticket's full message thread.
      .map(({ messages, ...rest }) => {
        void messages
        return rest
      })
    return ok(paginate(filtered, params)) as ApiResponse<T>
  }
  if (path === '/api/ticket/' && method === 'POST') {
    const title = String(body.title ?? '').trim()
    const content = String(body.content ?? '').trim()
    if (!title || title.length > 100)
      return fail('标题长度为 1-100 字符') as ApiResponse<T>
    if (!content || content.length > 2000)
      return fail('描述长度为 1-2000 字符') as ApiResponse<T>
    const ts = Math.floor(Date.now() / 1000)
    const ticket: TicketItem = {
      id: mockRuntime.nextTicketId++,
      title,
      category: String(body.category ?? 'other') as TicketCategory,
      priority: String(body.priority ?? 'normal') as TicketPriority,
      status: 'open',
      reply_count: 1,
      last_reply_role: 'user',
      model_id: body.model_id ? String(body.model_id) : undefined,
      request_id: body.request_id ? String(body.request_id) : undefined,
      created: ts,
      updated: ts,
      messages: [
        {
          id: mockRuntime.nextMessageId++,
          role: 'user',
          content,
          images: Array.isArray(body.images) ? (body.images as string[]) : [],
          created: ts,
        },
      ],
    }
    tickets.unshift(ticket)
    return ok({ id: ticket.id, ticket }) as ApiResponse<T>
  }

  const ticketIdMatch = path.match(/^\/api\/ticket\/(\d+)(\/reply|\/status)?$/)
  if (ticketIdMatch) {
    const id = Number(ticketIdMatch[1])
    const ticket = tickets.find((t) => t.id === id)
    if (!ticket) return fail('工单不存在') as ApiResponse<T>
    const sub = ticketIdMatch[2]

    if (!sub && method === 'GET') {
      const { messages, ...rest } = ticket
      return ok({ ticket: rest, messages }) as ApiResponse<T>
    }

    if (sub === '/reply' && method === 'POST') {
      if (ticket.status === 'closed') {
        return fail('工单已关闭，无法回复') as ApiResponse<T>
      }
      const content = String(body.content ?? '').trim()
      if (!content || content.length > 2000) {
        return fail('回复长度为 1-2000 字符') as ApiResponse<T>
      }
      const message: TicketMessage = {
        id: mockRuntime.nextMessageId++,
        role: 'user',
        content,
        images: Array.isArray(body.images) ? (body.images as string[]) : [],
        created: Math.floor(Date.now() / 1000),
      }
      ticket.messages.push(message)
      ticket.status = 'open'
      ticket.last_reply_role = 'user'
      ticket.reply_count = ticket.messages.length
      ticket.updated = message.created
      return ok({ message }) as ApiResponse<T>
    }

    if (sub === '/status' && method === 'PUT') {
      const next = String(body.status ?? '') as 'open' | 'closed'
      if (next !== 'open' && next !== 'closed') {
        return fail('无效的工单状态') as ApiResponse<T>
      }
      const ts = Math.floor(Date.now() / 1000)
      ticket.status = next
      ticket.updated = ts
      ticket.messages.push({
        id: mockRuntime.nextMessageId++,
        role: 'system',
        content: next === 'closed' ? '工单已关闭' : '工单已重新开启',
        images: [],
        created: ts,
      })
      ticket.reply_count = ticket.messages.length
      const { messages, ...rest } = ticket
      void messages
      return ok({ ticket: rest }) as ApiResponse<T>
    }
  }

  /* ---------------- activity center ---------------- */
  if (path === '/api/activity/self' && method === 'GET') {
    requireAuth(ctx)
    return ok({ activities, summary: activitySummary }) as ApiResponse<T>
  }
  if (path === '/api/activity/checkin' && method === 'POST') {
    requireAuth(ctx)
    const act = activities.find((a) => a.kind === 'checkin')
    if (!act || act.kind !== 'checkin')
      return fail('活动不存在') as ApiResponse<T>
    if (act.checkin.todayClaimed) return fail('今日已签到') as ApiResponse<T>
    const day = act.checkin.days[act.checkin.streak % act.checkin.days.length]
    day.done = true
    act.checkin.streak += 1
    act.checkin.todayClaimed = true
    act.checkin.total_days += 1
    act.checkin.month_days += 1
    act.checkin.total_reward += day.reward
    act.checkin.month_reward += day.reward
    // Mark today's week entry as claimed
    const todayEntry = act.checkin.week_entries.find((e) => e.today)
    if (todayEntry) {
      todayEntry.claimed = true
      todayEntry.reward = day.reward
    }
    return ok({
      reward: day.reward,
      streak: act.checkin.streak,
    }) as ApiResponse<T>
  }
  if (path === '/api/activity/claim' && method === 'POST') {
    requireAuth(ctx)
    const id = Number(body.activity_id)
    const act = activities.find((a) => a.id === id)
    if (!act) return fail('活动不存在') as ApiResponse<T>

    if (act.kind === 'newcomer') {
      const taskId = body.task_id ? String(body.task_id) : null
      if (taskId) {
        const task = act.newcomer.tasks.find((t) => t.id === taskId)
        if (!task) return fail('任务不存在') as ApiResponse<T>
        if (task.done) return fail('该任务已领取') as ApiResponse<T>
        task.done = true
        return ok({
          message: '领取成功',
          reward: task.reward,
        }) as ApiResponse<T>
      }
      if (act.newcomer.claimed) return fail('礼包已领取') as ApiResponse<T>
      const reward = act.newcomer.tasks
        .filter((t) => !t.done)
        .reduce((s, t) => s + t.reward, 0)
      act.newcomer.tasks.forEach((t) => (t.done = true))
      act.newcomer.claimed = true
      return ok({ message: '领取成功', reward }) as ApiResponse<T>
    }

    return fail('该活动不支持领取') as ApiResponse<T>
  }

  /* ---------------- marketplace (buy / sell) ---------------- */
  if (path === '/api/market/catalog' && method === 'GET') {
    // Buy side: only listed (active) public offers; the user's own listings are
    // served separately via /self/listings so the sell console owns their state.
    const listings = marketListings.filter(
      (l) => l.status === 'active' && l.ownerUid == null
    )
    return ok({
      listings,
      merchants: marketMerchants,
      channels: marketplaceChannels,
      vendors: [...new Set(listings.flatMap((l) => l.modelVendors))].sort(),
      // Full-catalog stats: computed over ALL public active listings, so they
      // stay stable regardless of client-side filtering.
      meta: {
        merchantCount: new Set(listings.map((l) => l.merchantId)).size,
        channelCount: listings.length,
        avgAvailability:
          listings.length > 0
            ? Math.round(
                (listings.reduce((s, l) => s + l.availability, 0) /
                  listings.length) *
                  10
              ) / 10
            : 0,
      },
    }) as ApiResponse<T>
  }

  /* ---------------- marketplace (my channels) ---------------- */
  if (path === '/api/market/my-channels' && method === 'GET') {
    return ok({ channels: myChannels }) as ApiResponse<T>
  }

  const myChannelMatch = path.match(/^\/api\/market\/my-channels\/(\d+)$/)
  if (myChannelMatch) {
    const channel = myChannels.find((c) => c.id === Number(myChannelMatch[1]))
    if (!channel) return fail('渠道不存在') as ApiResponse<T>

    if (method === 'PUT') {
      channel.status = channel.status === 'active' ? 'disabled' : 'active'
      return ok({
        channel,
        message: channel.status === 'active' ? '渠道已启用' : '渠道已禁用',
      }) as ApiResponse<T>
    }

    if (method === 'DELETE') {
      myChannels.splice(myChannels.indexOf(channel), 1)
      return ok({ message: '渠道已移除' }) as ApiResponse<T>
    }
  }

  const addAllMatch = path.match(/^\/api\/market\/merchant\/(\d+)\/add-all$/)
  if (addAllMatch && method === 'POST') {
    const merchantId = Number(addAllMatch[1])
    if (!marketMerchants.some((m) => m.id === merchantId)) {
      return fail('商家不存在') as ApiResponse<T>
    }
    const candidates = marketListings.filter(
      (l) =>
        l.merchantId === merchantId &&
        l.status === 'active' &&
        l.ownerUid == null &&
        !myChannels.some((c) => c.listingId === l.id)
    )
    if (candidates.length === 0)
      return fail('该商家渠道均已添加') as ApiResponse<T>
    candidates.forEach((l) => {
      addMyChannel(l)
      l.reviewCount += 1
    })
    return ok({ added: candidates.length }) as ApiResponse<T>
  }

  if (path === '/api/market/self/listings' && method === 'GET') {
    const uid = Number(ctx.headers['X-Ren2Hub-Demo-User'])
    const mine = marketListings.filter((l) => l.ownerUid === uid)
    const active = mine.filter((l) => l.status === 'active')
    return ok({
      listings: mine,
      stats: {
        active: active.length,
        totalSales: mine.reduce((s, l) => s + l.reviewCount, 0),
        // Pending settlement earnings, in quota units (see /api/market/settle).
        pendingEarnings: mockRuntime.marketSelfEarnings,
        rating:
          mine.length > 0
            ? Math.round(
                (mine.reduce((s, l) => s + l.rating, 0) / mine.length) * 10
              ) / 10
            : 0,
      },
    }) as ApiResponse<T>
  }

  if (path === '/api/market/listing' && method === 'POST') {
    const uid = Number(ctx.headers['X-Ren2Hub-Demo-User'])
    const title = String(body.title ?? '').trim()
    if (!title || title.length > 60)
      return fail('商品名称长度为 1-60 字符') as ApiResponse<T>
    const priceUSD = Number(body.priceUSD ?? 0)
    if (!(priceUSD > 0)) return fail('价格必须大于 0') as ApiResponse<T>
    const models = Array.isArray(body.supportedModels)
      ? (body.supportedModels as string[])
      : []
    if (models.length === 0)
      return fail('请至少选择一个支持模型') as ApiResponse<T>
    const ts = Math.floor(Date.now() / 1000)
    const listing: MarketListing = {
      id: mockRuntime.nextListingId++,
      merchantId: 1,
      title,
      summary: String(body.summary ?? '').trim(),
      source: String(body.source ?? marketplaceChannels[0]),
      availability: 99,
      supportedModels: models,
      qcScore: 95,
      tags: Array.isArray(body.tags) ? (body.tags as string[]) : [],
      priceUSD,
      type: String(body.type ?? 'chat') as MarketModelType,
      listedAt: ts,
      rating: 0,
      reviewCount: 0,
      // New offers enter review before going live, mirroring the real backend.
      status: 'reviewing',
      ownerUid: uid,
      modelVendors: [
        ...new Set(models.map((m) => modelVendorMap[m]).filter(Boolean)),
      ],
    }
    marketListings.unshift(listing)
    return ok({ listing, message: '供货已提交审核' }) as ApiResponse<T>
  }

  const listingIdMatch = path.match(/^\/api\/market\/listing\/(\d+)(\/add)?$/)
  if (listingIdMatch) {
    const id = Number(listingIdMatch[1])
    const listing = marketListings.find((l) => l.id === id)
    if (!listing) return fail('供货不存在') as ApiResponse<T>
    const uid = Number(ctx.headers['X-Ren2Hub-Demo-User'])

    if (listingIdMatch[2] === '/add' && method === 'POST') {
      // Buyer adds a listing to their manageable channel list (我的渠道).
      if (myChannels.some((c) => c.listingId === listing.id)) {
        return fail('该渠道已添加') as ApiResponse<T>
      }
      addMyChannel(listing)
      listing.reviewCount += 1
      return ok({
        message: `已添加「${listing.title}」到我的渠道`,
      }) as ApiResponse<T>
    }

    // Owner-only mutations below.
    if (listing.ownerUid !== uid)
      return fail('无权操作该供货') as ApiResponse<T>

    if (method === 'PUT') {
      Object.assign(listing, {
        title: body.title !== undefined ? String(body.title) : listing.title,
        summary:
          body.summary !== undefined ? String(body.summary) : listing.summary,
        source:
          body.source !== undefined ? String(body.source) : listing.source,
        supportedModels: Array.isArray(body.supportedModels)
          ? (body.supportedModels as string[])
          : listing.supportedModels,
        tags: Array.isArray(body.tags) ? (body.tags as string[]) : listing.tags,
        priceUSD:
          body.priceUSD !== undefined
            ? Number(body.priceUSD)
            : listing.priceUSD,
        type:
          body.type !== undefined
            ? (String(body.type) as MarketModelType)
            : listing.type,
        status:
          body.status !== undefined
            ? (String(body.status) as ListingStatus)
            : listing.status,
      })
      return ok({ listing, message: '供货已更新' }) as ApiResponse<T>
    }

    if (method === 'DELETE') {
      marketListings.splice(marketListings.indexOf(listing), 1)
      return ok({ message: '供货已下架' }) as ApiResponse<T>
    }
  }

  if (path === '/api/market/settle' && method === 'POST') {
    if (mockRuntime.marketSelfEarnings <= 0)
      return fail('暂无可结算收益') as ApiResponse<T>
    const settled = mockRuntime.marketSelfEarnings
    mockRuntime.marketSelfEarnings = 0
    return ok({
      message: '收益已转入账户余额',
      quota: settled,
    }) as ApiResponse<T>
  }

  /* ---------------- alchemy lab (UI prototype) ---------------- */
  // Chat: landing needs model picks + starter cards + the conversation list;
  // opening a conversation fetches its messages by id. Contract §lab.
  if (path === '/api/lab/chat/landing' && method === 'GET') {
    requireAuth(ctx)
    return ok({
      models: labModelPicks,
      starters: labStarters,
      conversations: chatConversations.map(({ messages, ...rest }) => {
        void messages
        return rest
      }),
    }) as ApiResponse<T>
  }
  if (path.startsWith('/api/lab/chat/conversation/') && method === 'GET') {
    requireAuth(ctx)
    const id = path.split('/').pop()
    const convo = chatConversations.find((c) => c.id === id)
    if (!convo) return fail('对话不存在') as ApiResponse<T>
    return ok(convo) as ApiResponse<T>
  }

  // Studio: shared image+video gallery plus the quick-tool shortcuts.
  if (path === '/api/lab/studio' && method === 'GET') {
    requireAuth(ctx)
    const kind = params.kind as string | undefined
    const works =
      kind === 'image' || kind === 'video'
        ? studioGallery.filter((w) => w.kind === kind)
        : studioGallery
    return ok({ works, tools: studioTools }) as ApiResponse<T>
  }

  // Assets: filter by kind tab; header shows the storage meter.
  if (path === '/api/lab/assets' && method === 'GET') {
    requireAuth(ctx)
    const kind = params.kind as string | undefined
    const items =
      kind && kind !== 'all'
        ? assetItems.filter((a) =>
            kind === 'media'
              ? a.kind === 'image' || a.kind === 'video'
              : a.kind === kind
          )
        : assetItems
    return ok({ items, storage: assetStorage }) as ApiResponse<T>
  }

  if (path === '/api/lab/notes' && method === 'GET') {
    requireAuth(ctx)
    return ok({ items: noteItems }) as ApiResponse<T>
  }

  if (path === '/api/lab/community' && method === 'GET') {
    requireAuth(ctx)
    const category = params.category as CommunityCategory | 'all' | undefined
    const sort = params.sort as string | undefined
    let works = communityWorks.slice()
    if (category && category !== 'all')
      works = works.filter((w) => w.category === category)
    if (sort === 'featured') works = works.filter((w) => w.featured)
    return ok({ works }) as ApiResponse<T>
  }

  if (path === '/api/lab/plugins' && method === 'GET') {
    requireAuth(ctx)
    return ok({
      plugins: installedPlugins,
      mcp: mcpServers,
      skills: skillItems,
      market: marketPlugins,
    }) as ApiResponse<T>
  }

  /* ---------------- invoices (fapiao / 开票) ---------------- */

  if (path === '/api/invoice/self' && method === 'GET') {
    requireAuth(ctx)
    const result = paginate(invoices, params)
    return ok(result) as ApiResponse<T>
  }

  if (path === '/api/invoice/apply' && method === 'POST') {
    requireAuth(ctx)
    const title = String(body.title ?? '').trim()
    const amount = Number(body.amount ?? 0)
    if (!title) return fail('发票抬头不能为空') as ApiResponse<T>
    if (!amount || amount < 200)
      return fail('开票金额最低 200 元') as ApiResponse<T>
    const item = addInvoice({
      title,
      tax_id: String(body.tax_id ?? '').trim(),
      amount,
      email: String(body.email ?? '').trim(),
      note: String(body.note ?? '').trim(),
    })
    return ok({
      invoice: item,
      message: '申请已提交，等待管理员审核',
    }) as ApiResponse<T>
  }

  if (
    path.startsWith('/api/invoice/') &&
    path.endsWith('/pdf') &&
    method === 'GET'
  ) {
    requireAuth(ctx)
    const id = Number(path.split('/')[3])
    const inv = invoices.find((i) => i.id === id)
    if (!inv) return fail('开票记录不存在') as ApiResponse<T>
    if (inv.status !== 'issued') return fail('发票尚未开具') as ApiResponse<T>
    return ok({ pdf_url: inv.pdf_url }) as ApiResponse<T>
  }

  /* ---------------- farm (RT农家乐) ---------------- */
  if (path === '/api/farm/self' && method === 'GET') {
    return ok({
      state: farmState,
      plots: farmPlots,
      animals: ranchAnimals,
      fishing: fishingState,
      pet: myPet,
      mine: mineState,
    }) as ApiResponse<T>
  }

  if (path === '/api/farm/leader' && method === 'GET') {
    const period = String(params.period ?? 'day') as LeaderPeriod
    return ok({ entries: leaderboard, period }) as ApiResponse<T>
  }

  if (path === '/api/farm/rebate' && method === 'GET') {
    return ok({ tiers: rebateTiers, state: rebateState }) as ApiResponse<T>
  }

  const farmHarvestMatch = path.match(/^\/api\/farm\/harvest\/(\d+)$/)
  if (farmHarvestMatch && method === 'POST') {
    const plotId = Number(farmHarvestMatch[1])
    const plot = farmPlots.find((p) => p.id === plotId)
    if (!plot) return fail('地块不存在') as ApiResponse<T>
    if (plot.stage !== 'ready') return fail('作物尚未成熟') as ApiResponse<T>
    const gained = plot.yield_quota
    farmState.coins += gained
    plot.stage = 'empty'
    plot.seed = null
    plot.planted_at = null
    plot.harvest_at = null
    plot.yield_quota = 0
    return ok({ coins: farmState.coins, gained }) as ApiResponse<T>
  }

  const farmFeedAnimalMatch = path.match(/^\/api\/farm\/feed\/animal\/(\d+)$/)
  if (farmFeedAnimalMatch && method === 'POST') {
    const animalId = Number(farmFeedAnimalMatch[1])
    const animal = ranchAnimals.find((a) => a.id === animalId)
    if (!animal) return fail('动物不存在') as ApiResponse<T>
    animal.fed_at = Math.floor(Date.now() / 1000)
    animal.mood = 100
    return ok({ animal }) as ApiResponse<T>
  }

  const farmCollectAnimalMatch = path.match(
    /^\/api\/farm\/collect\/animal\/(\d+)$/
  )
  if (farmCollectAnimalMatch && method === 'POST') {
    const animalId = Number(farmCollectAnimalMatch[1])
    const animal = ranchAnimals.find((a) => a.id === animalId)
    if (!animal) return fail('动物不存在') as ApiResponse<T>
    if (!animal.yield_ready) return fail('暂无可收取的产出') as ApiResponse<T>
    const gained = animal.yield_quota
    farmState.coins += gained
    animal.yield_ready = false
    return ok({ coins: farmState.coins, gained }) as ApiResponse<T>
  }

  if (path === '/api/farm/fish' && method === 'POST') {
    if (fishingState.daily_left <= 0)
      return fail('今日钓鱼次数已用完') as ApiResponse<T>
    const catchPool = [
      { name: '小鱼干', quota: 5000, rarity: 'common' as const, emoji: '🐟' },
      { name: '草鱼', quota: 15000, rarity: 'common' as const, emoji: '🐠' },
      { name: '鲤鱼', quota: 30000, rarity: 'common' as const, emoji: '🐡' },
      { name: '金鲤鱼', quota: 80000, rarity: 'rare' as const, emoji: '🐠' },
      { name: '锦鲤', quota: 200000, rarity: 'rare' as const, emoji: '🎏' },
      {
        name: '龙鱼',
        quota: 1000000,
        rarity: 'legendary' as const,
        emoji: '🐉',
      },
    ]
    // Pick rarity first: common 70%, rare 25%, legendary 5%
    const rarityRoll = Math.random() * 100
    const rarity =
      rarityRoll < 70 ? 'common' : rarityRoll < 95 ? 'rare' : 'legendary'
    const pool = catchPool.filter((f) => f.rarity === rarity)
    const caught = pool[Math.floor(Math.random() * pool.length)]
    fishingState.daily_left -= 1
    fishingState.last_catch = caught
    return ok({
      catch: fishingState.last_catch,
      daily_left: fishingState.daily_left,
    }) as ApiResponse<T>
  }

  if (path === '/api/farm/feed/pet' && method === 'POST') {
    myPet.fed_today = true
    myPet.energy = 100
    return ok({ pet: myPet }) as ApiResponse<T>
  }

  /* ---------------- bigame (无趣大游戏) ---------------- */
  if (path === '/api/bigame/self' && method === 'GET') {
    return ok({
      wallet: gameWallet,
      milestones,
      prizes: spinPrizes,
      records: prizeRecords,
    }) as ApiResponse<T>
  }

  if (path === '/api/bigame/spin' && method === 'POST') {
    const SPIN_COST = 5
    if (gameWallet.balance < SPIN_COST)
      return fail('游戏币不足，无法转盘') as ApiResponse<T>
    gameWallet.balance -= SPIN_COST
    gameWallet.total_spent += SPIN_COST
    // Weighted random pick
    const totalWeight = spinPrizes.reduce((s, p) => s + p.weight, 0)
    let roll = Math.random() * totalWeight
    const prize =
      spinPrizes.find((p) => (roll -= p.weight) < 0) ?? spinPrizes[0]
    if (prize.type === 'coins') {
      gameWallet.balance += prize.value
      gameWallet.total_earned += prize.value
    }
    const record: PrizeRecord = {
      id: prizeRecords.length + 1,
      source: 'spin',
      prize_label: prize.label,
      rarity: 'common',
      value: prize.value,
      type: prize.type === 'nothing' ? 'coins' : prize.type,
      created: Math.floor(Date.now() / 1000),
    }
    prizeRecords.unshift(record)
    return ok({ prize, wallet: gameWallet }) as ApiResponse<T>
  }

  if (path === '/api/bigame/box' && method === 'POST') {
    const BOX_COST = 10
    if (gameWallet.balance < BOX_COST)
      return fail('游戏币不足，无法开盲盒') as ApiResponse<T>
    gameWallet.balance -= BOX_COST
    gameWallet.total_spent += BOX_COST
    // Pick rarity tier first by weight: common 70%, rare 20%, epic 8%, legendary 2%
    const rarityRoll = Math.random() * 100
    const rarity =
      rarityRoll < 70
        ? 'common'
        : rarityRoll < 90
          ? 'rare'
          : rarityRoll < 98
            ? 'epic'
            : 'legendary'
    const pool = blindBoxPrizes.filter((p) => p.rarity === rarity)
    const totalWeight = pool.reduce((s, p) => s + p.weight, 0)
    let roll = Math.random() * totalWeight
    const prize = pool.find((p) => (roll -= p.weight) < 0) ?? pool[0]
    if (prize.type === 'coins') {
      gameWallet.balance += prize.value
      gameWallet.total_earned += prize.value
    }
    const record: PrizeRecord = {
      id: prizeRecords.length + 1,
      source: 'box',
      prize_label: prize.label,
      rarity: prize.rarity,
      value: prize.value,
      type: prize.type,
      created: Math.floor(Date.now() / 1000),
    }
    prizeRecords.unshift(record)
    return ok({ prize, wallet: gameWallet }) as ApiResponse<T>
  }

  if (path === '/api/bigame/milestone/claim' && method === 'POST') {
    const id = String(body.id ?? '')
    const milestone = milestones.find((m) => m.id === id)
    if (!milestone) return fail('里程碑不存在') as ApiResponse<T>
    if (milestone.claimed) return fail('该里程碑奖励已领取') as ApiResponse<T>
    if (milestone.current < milestone.target)
      return fail('尚未达成该里程碑') as ApiResponse<T>
    milestone.claimed = true
    gameWallet.balance += milestone.reward
    gameWallet.total_earned += milestone.reward
    return ok({ wallet: gameWallet }) as ApiResponse<T>
  }

  throw new ApiError(`接口不存在：${method} ${path}`, { status: 404 })
}
