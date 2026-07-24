/**
 * Token routing types:
 * - manual: the user selects platform and marketplace channels
 * - auto:   the system computes the best channels across both pools
 */
export type TokenType = 'manual' | 'auto'

/** One upstream channel bound to a token; array order = routing priority. */
export interface TokenChannel {
  name: string
  enabled: boolean
  weight?: number // load-balance weight (only meaningful when load_balance)
}

export interface TokenItem {
  id: number
  name: string
  key: string
  type: TokenType
  status: 1 | 2 // 1 enabled · 2 disabled
  used_quota: number
  remain_quota: number
  unlimited: boolean
  group: string
  model_limits: string[]
  ip_limits: string[]
  rate_limit: number // requests / minute, 0 = unlimited
  max_ratio?: number // max accepted channel price ratio
  load_balance: boolean
  channels: TokenChannel[] // order = priority (auto: computed server-side)
  expired_time: number // -1 = never
  created_time: number
}

export type LogType =
  'consume' | 'topup' | 'refund' | 'manage' | 'error' | 'system'

export interface LogItem {
  id: number
  type: LogType
  token_name: string // 使用的 API 令牌名称
  model: string
  channel: string // 渠道名称
  prompt_tokens: number
  completion_tokens: number
  quota: number
  latency: number // 延迟（秒）
  tps: number // tokens per second (0 表示不适用)
  content: string
  created: number
}

export interface TopupRecord {
  id: number
  trade_no: string
  amount: number // USD purchased
  money: number // quota credited
  method: 'epay' | 'stripe' | 'creem' | 'redeem'
  status: 'success' | 'pending' | 'failed'
  created: number
}

export interface Plan {
  id: number
  name: string
  price: number
  quota: number
  duration_days: number
  features: string[]
  gradient: 'accent' | 'signal' | 'support'
  recommended?: boolean
}

/* ---------------- model plaza (market) ---------------- */

export type MarketModelType =
  'chat' | 'image' | 'embedding' | 'rerank' | 'audio' | 'video'

export type MarketBilling = 'token' | 'per_call' | 'tiered'

export interface MarketPriceTier {
  label: string
  input: number
  output: number
}

export interface MarketModelPrice {
  input?: number // $ / 1M tokens
  output?: number // $ / 1M tokens
  cache_read?: number // $ / 1M tokens
  per_call?: number // $ / call
  tiers?: MarketPriceTier[] // tiered billing breakdown
}

export interface MarketModel {
  id: number
  name: string
  vendor: string
  type: MarketModelType
  billing: MarketBilling
  price: MarketModelPrice
  context: number // context window in tokens (0 = n/a)
  tagline: string
  latency: number // seconds, snapshot
  tps: number // tokens/s, snapshot (0 = n/a)
  health: number // 0-100 → drives the health meter + tone
  channels: string[]
}

export type InviteRecordStatus = 'valid' | 'pending' | 'invalid'

export interface InviteRecord {
  id: number
  invitee: string
  reward: number
  status: InviteRecordStatus
  created: number
}

export interface InviteQualification {
  token_used: number
  token_required: number
  topup_total: number
  topup_required: number
  qualified: boolean
}

export interface InviteUnlockChannel {
  id: string
  name: string
  detail: string
  unlocked: boolean
}

export interface InviteMonthPoint {
  month: string
  new_count: number
  cumulative: number
}

export interface InviteInfo {
  code: string
  invited: number
  rate: number // rebate ratio, e.g. 0.02 = 2%
  reward_total: number // lifetime reward quota earned
  transferable: number // reward quota available to move into balance
  pending_reward: number // reward quota awaiting settlement
  qualification: InviteQualification
  unlock_channels: InviteUnlockChannel[]
  monthly_series: InviteMonthPoint[]
  records: InviteRecord[]
}

/* ---------------- tickets (support ledger) ---------------- */

export type TicketStatus = 'open' | 'replied' | 'closed'

export type TicketCategory = 'billing' | 'api' | 'model' | 'account' | 'other'

export type TicketPriority = 'low' | 'normal' | 'high'

export type TicketRole = 'user' | 'support' | 'system'

// Which team a support reply comes from. Shown as the message author label;
// user messages carry no department (their name is intentionally hidden).
export type TicketDepartment = 'support' | 'tech' | 'finance' | 'legal'

export interface TicketMessage {
  id: number
  role: TicketRole
  // Only meaningful when role === 'support'; falls back to 'support' (客服).
  department?: TicketDepartment
  content: string
  images: string[]
  created: number
}

export interface TicketItem {
  id: number
  title: string
  category: TicketCategory
  priority: TicketPriority
  status: TicketStatus
  reply_count: number
  last_reply_role: 'user' | 'support'
  model_id?: string
  request_id?: string
  created: number
  updated: number
  messages: TicketMessage[]
}

/* ---------------- activity center ---------------- */

export type ActivityKind = 'checkin' | 'newcomer' | 'invite'

export type ActivityStatus = 'ongoing' | 'upcoming' | 'ended'

export type ActivityGradient = 'accent' | 'signal' | 'support'

export interface ActivityBase {
  id: number
  kind: ActivityKind
  title: string
  tagline: string
  status: ActivityStatus
  gradient: ActivityGradient
  badgeKey?: 'hot' | 'new' | 'ending'
  start: number // epoch seconds
  end: number // epoch seconds
  icon: string // 24×24 lucide path
}

export interface CheckinDay {
  done: boolean
  reward: number
}

export interface WeekEntry {
  date: string // "07/21"
  weekday: string // "MON" | "TUE" | ...
  reward: number // quota units; 0 = not claimed
  claimed: boolean
  today: boolean
}

export interface CheckinPayload {
  days: CheckinDay[]
  todayClaimed: boolean
  streak: number
  total_days: number
  month_days: number
  month_days_total: number
  total_reward: number
  month_reward: number
  best_streak: number
  week_entries: WeekEntry[]
}

export interface NewcomerTask {
  id: string
  labelKey: string
  reward: number
  done: boolean
}

export interface NewcomerPayload {
  tasks: NewcomerTask[]
  claimed: boolean
}

export interface InviteActivityPayload {
  invited: number
  reward_total: number
  rate: number
}

export type Activity = ActivityBase &
  (
    | { kind: 'checkin'; checkin: CheckinPayload }
    | { kind: 'newcomer'; newcomer: NewcomerPayload }
    | { kind: 'invite'; invite: InviteActivityPayload }
  )

export interface ActivitySummary {
  claimable: number
  reward_earned: number
  ongoing: number
}

/* ---------------- marketplace (channel-supply trading market) ---------------- */

/**
 * The "market" nav entry is a two-sided trading market (buy / sell), distinct
 * from the read-only Model Plaza (models). Merchants list channel-supply
 * offers; buyers browse, compare and add them. Base prices are stored in USD
 * and converted for display via USD_TO_CNY. qcScore is a 0–100 stability score.
 */
export type MarketSide = 'buy' | 'sell' | 'mine'

/**
 * Merchant scale tiers (ascending size): vendor(摊贩) < workshop(作坊) <
 * studio(工作室) < empire(夯可败国). `platform` is the official first-party
 * supply and sits outside the third-party ladder.
 */
export type MerchantScale =
  'platform' | 'vendor' | 'workshop' | 'studio' | 'empire'

export type ListingStatus = 'active' | 'reviewing' | 'delisted'

export type Currency = 'CNY' | 'USD'

export interface MerchantComment {
  id: number
  user: string
  content: string
  createdAt: number
}

export interface Merchant {
  id: number
  name: string
  scale: MerchantScale
  comments: MerchantComment[] // newest first
  channelCount: number
  joinedAt: number
  verified: boolean
}

export interface MarketListing {
  id: number
  merchantId: number
  title: string
  summary: string
  source: string // upstream channel the supply routes through
  availability: number // uptime %
  supportedModels: string[]
  qcScore: number // 0–100 stability score
  tags: string[]
  priceUSD: number // $ / 1M tokens (chat, embedding) or $ / call (image, audio, video)
  type: MarketModelType
  listedAt: number
  rating: number // 0–5 listing rating
  reviewCount: number
  status: ListingStatus
  ownerUid?: number // set when the listing belongs to the current user (sell side)
  earningsUSD?: number // cumulative seller earnings (sell side only)
  modelVendors: string[] // distinct AI vendors derived from supportedModels
  qcImages?: string[] // QC test screenshots attached under the QC score
}

/* ------------------------------------------------------------------ */
/* my channels — marketplace listings the user added to their account  */
/* ------------------------------------------------------------------ */

export type MyChannelStatus = 'active' | 'disabled'

export interface MyChannel {
  id: number
  listingId: number
  merchantId: number
  merchantName: string
  title: string // listing title, for display
  supportedModels: string[]
  status: MyChannelStatus
  addedAt: number
}

/* ------------------------------------------------------------------ */
/* invoices — online invoicing / fapiao requests                        */
/* ------------------------------------------------------------------ */

export type InvoiceStatus = 'pending' | 'issued' | 'rejected'

export interface InvoiceItem {
  id: number
  title: string // 发票抬头（公司名或个人姓名）
  tax_id: string // 税号（企业填纳税人识别号，个人可留空）
  amount: number // 开票金额，单位：元（CNY）
  email: string // 接收发票的邮箱（可选）
  note: string // 备注
  status: InvoiceStatus
  pdf_url: string // 仅 issued 状态时非空
  reject_reason: string // 仅 rejected 状态时非空
  created: number
  updated: number
}
export type TokenSummary = Omit<TokenItem, 'key'> & {
  key_preview: string
}

export interface CurrentSubscription {
  plan_id: number
  name: string
  total_quota: number
  remain_quota: number
  expire_time: number
  auto_renew: boolean
}
