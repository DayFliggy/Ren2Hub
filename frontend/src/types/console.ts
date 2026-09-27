/**
 * Token routing types:
 * - manual: the user selects platform and marketplace channels
 * - auto:   the system computes the best channels across both pools
 */
export type TokenType = 'manual' | 'auto'

/** One upstream channel bound to a token; array order = routing priority. */
export interface TokenChannel {
  channel_id: number
  enabled: boolean
  weight?: number // load-balance weight (only meaningful when load_balance)
}

export interface TokenItem {
  id: number
  name: string
  key: string
  type: TokenType
  conversion_policy?: import('@/types/gateway').ConversionPolicy
  status: 1 | 2 // 1 enabled · 2 disabled
  used_quota: string
  available_quota: string | null
  reserved_quota: string
  quota_version: number
  unlimited: boolean
  model_limits: string[]
  ip_limits: string[]
  rate_limit: number // requests / minute, 0 = unlimited
  max_ratio?: string // max accepted channel price ratio
  load_balance: boolean
  channels: TokenChannel[] // order = priority (auto: computed server-side)
  expired_time: number // -1 = never
  created_time: number
}

export type AdminChannelStatus = 1 | 2 | 3

export type AdminChannelSortBy =
  'id' | 'name' | 'priority' | 'balance' | 'response_time'

export type AdminChannelSortOrder = 'asc' | 'desc'

export interface AdminChannelLabMatch {
  slug: string
  name: string
  confidence: number
  source: string
}

export interface AdminChannelModelMatch {
  input_model: string
  real_model: string
  canonical_id?: string
  lab_slug?: string
  confidence: number
  source: string
}

export interface AdminChannel extends Record<string, unknown> {
  channel_info?: {
    is_multi_key: boolean
    multi_key_mode: 'random' | 'polling'
    multi_key_size?: number
  }
  id: number
  name: string
  type: number
  supplier: string
  status: AdminChannelStatus
  priority: number
  weight: number
  // Capacity & ratio fields (Phase A backend fields)
  capacity_used: number
  capacity_total: number
  channel_ratio: string
  used_quota: string
  balance: number
  response_time: number
  test_time: number
  base_url: string
  models: string
  model_mapping: string
  lab_group_slug?: string
  lab_group_name?: string
  lab_matches?: AdminChannelLabMatch[]
  lab_models?: AdminChannelModelMatch[]
  lab_unresolved_count?: number
  lab_catalog_version?: string
}

export interface AdminChannelCreateInput {
  setting?: string
  settings?: string
  param_override?: string
  header_override?: string
  multi_key_mode?: 'random' | 'polling'
  key_mode?: 'append' | 'replace'
  name: string
  type: number
  status: Extract<AdminChannelStatus, 1 | 2>
  // Credentials
  key: string
  base_url: string
  // Models (no group: creation uses the backend default routing group,
  // edits preserve the stored value verbatim)
  models: string
  model_mapping: string
  // Routing
  priority: number
  weight: number
  // Capacity & ratio
  capacity_total: number
  channel_ratio: string
}

export interface AdminChannelCreateOptions {
  batchKeys?: boolean
  multiKey?: boolean
  multiKeyMode?: 'random' | 'polling'
}

export type AdminChannelUpdateInput = Omit<
  AdminChannelCreateInput,
  'status' | 'key'
> & {
  /** Sensitive field: include only when replacing the stored key. */
  key?: string
}

/** Params for discovering a channel's model list from its upstream. */
export type AdminChannelFetchModelsParams =
  | { channelId: number; advancedCustom?: string; baseUrl?: string }
  | { type: number; key: string; baseUrl: string; advancedCustom?: string }

export interface AdminChannelPage {
  items: AdminChannel[]
  total: number
  page: number
  page_size: number
  type_counts: Record<string, number>
}

/**
 * Mirrors the backend role ladder in `common/constants.go`:
 * RoleGuestUser=0 · RoleCommonUser=1 · RoleAdminUser=10 · RoleRootUser=100.
 */
export type AdminUserRole = 0 | 1 | 10 | 100

/** 1 enabled · 2 disabled — same convention as channels and tokens. */
export type AdminUserStatus = 1 | 2

export type AdminUserSortBy =
  | 'id'
  | 'username'
  | 'available_quota'
  | 'used_quota'
  | 'created_time'
  | 'last_login_time'

export type AdminUserSortOrder = 'asc' | 'desc'

export interface AdminUser extends Record<string, unknown> {
  id: number
  username: string
  display_name: string
  email: string
  role: AdminUserRole
  status: AdminUserStatus
  available_quota: string
  reserved_quota: string
  used_quota: string
  quota_version: number
  request_count: number
  invited_count: number
  affiliate_quota: string
  /** 0 = registered without an inviter. */
  inviter_id: number
  created_time: number
  /** 0 = never signed in. */
  last_login_time: number
}

export interface AdminUserCreateInput {
  username: string
  display_name: string
  email: string
  password: string
  role: AdminUserRole
  status: AdminUserStatus
}

/** Status is owned by the dedicated toggle route, not by the edit form. */
export type AdminUserUpdateInput = Omit<
  AdminUserCreateInput,
  'status' | 'password'
> & {
  password?: string
}

export interface AdminUserPage {
  items: AdminUser[]
  total: number
  page: number
  page_size: number
  role_counts: Record<string, number>
  status_counts: Record<string, number>
}

export interface AdminAffiliateUser {
  id: number
  username: string
  status: AdminUserStatus
  code: string
  code_enabled: boolean
  code_custom: boolean
  rate_bps: number | null
  effective_rate_bps: number
  invite_limit: number | null
  invited_count: number
  available_reward: string
  frozen_reward: string
  total_reward: string
}

export interface AdminAffiliateUserUpdateInput {
  code?: string
  code_enabled?: boolean
  rate_bps?: number
  clear_rate?: boolean
  invite_limit?: number
  clear_invite_limit?: boolean
}

/* ---------------- administrator orders ---------------- */

/**
 * Payment lifecycle. `completed` is the only state a refund may act on;
 * `cancelled` and `expired` are both terminal unpaid outcomes, distinguished
 * because one is a user action and the other a gateway timeout.
 */
export type AdminOrderStatus = 'completed' | 'pending' | 'failed'

/** Order management currently covers wallet top-ups. */
export type AdminOrderType = 'topup'

/**
 * Historical providers remain visible for read-only order reconciliation.
 */
export type AdminOrderMethod =
  | 'epay'
  | 'gmpay'
  | 'stripe'
  | 'creem'
  | 'waffo'
  | 'waffo_pancake'
  | 'balance'
  | 'other'

export type AdminOrderPaymentRail = 'alipay' | 'wechat' | 'other'

export type AdminOrderCurrency = 'CNY' | 'USD'

export type AdminOrderSortBy = 'id' | 'amount' | 'created'

export type AdminOrderSortOrder = 'asc' | 'desc'

/** Trailing window the statistics tab aggregates over, in days. */
export type AdminOrderRange = 7 | 30 | 90

export interface AdminOrder extends Record<string, unknown> {
  id: number
  /** Gateway trade number, e.g. `sub2_20260725RHcR5xPa`. */
  order_no: string
  user_id: number
  username: string
  email: string
  /** Amount actually paid, denominated by `currency`. */
  amount: number
  /** Quota credited on completion. 0 for orders that never completed. */
  quota: string
  type: AdminOrderType
  currency: AdminOrderCurrency
  method: AdminOrderMethod
  payment_method: string
  status: AdminOrderStatus
  created: number
  /** 0 = never paid. */
  paid_at: number
}

export interface GmpayOrderEvidence {
  status: 'creating' | 'ready' | 'failed' | 'unknown' | 'settled'
  last_failure: string
  gateway_trade_no: string
  token: string
  /** Requested network; the callback does not attest to the final network. */
  network: string
  gateway_crypto_amount: string
  receive_address: string
  settled_crypto_amount: string
  settled_receive_address: string
  settled_transaction_id: string
}

export interface AdminOrderPage {
  items: AdminOrder[]
  total: number
  page: number
  page_size: number
  status_counts: Record<string, number>
  method_counts: Record<string, number>
  type_counts: Record<string, number>
  /** Completed Epay revenue across the whole filtered set, in CNY. */
  filtered_epay_revenue: number
}

export interface AdminOrderDailyPoint {
  /** `YYYY-MM-DD`, local time. */
  date: string
  revenue: number
  orders: number
}

export interface AdminOrderMethodShare {
  method: AdminOrderPaymentRail
  amount: number
  count: number
}

export interface AdminOrderSpender {
  user_id: number
  username: string
  email: string
  amount: number
  orders: number
}

export interface AdminOrderStats {
  range: AdminOrderRange
  /** Server clock when the window was aggregated. */
  generated_at: number
  currency: 'CNY'
  today_revenue: number
  today_orders: number
  /** Revenue over the requested window, not all-time. */
  total_revenue: number
  total_orders: number
  /** Mean completed Epay order value over the window; 0 when empty. */
  average_amount: number
  daily: AdminOrderDailyPoint[]
  payment_share: AdminOrderMethodShare[]
  top_spenders: AdminOrderSpender[]
}

export type LogType =
  'consume' | 'topup' | 'refund' | 'manage' | 'error' | 'system' | 'login'

export type LogRequestMode = 'stream' | 'sync'

export type LogOther = string | Record<string, unknown> | null

export interface LogItem {
  id: number
  type: LogType
  token_name: string // 使用的 API 令牌名称
  model: string
  channel: string // 渠道名称
  prompt_tokens: number
  completion_tokens: number
  /** Cache-token fields are optional until the production log API exposes them. */
  cache_read_tokens?: number | null
  cache_write_tokens?: number | null
  cache_ttl?: string | null
  /** Raw request metadata. Production logs currently expose this as JSON. */
  other?: LogOther
  /** Optional flattened metadata accepted by alternate API adapters. */
  reasoning_effort?: string | null
  fast_mode?: boolean | null
  service_tier?: string | null
  speed?: string | null
  quota: string
  latency: number // 延迟（秒）
  first_token_latency: number | null // 首字延迟（秒），仅流式请求适用
  request_mode: LogRequestMode | null // 非请求类日志为 null
  tps: number // tokens per second (0 表示不适用)
  content: string
  created: number
}

export type OperationLogKind = 'manage' | 'system' | 'login'

export interface OperationLogActor {
  id: number
  username: string
  role: number | null
  auth_method: string
}

export interface OperationLogRequest {
  method: string
  route: string
  path: string
  status: number | null
  success: boolean | null
}

export interface OperationLogItem extends Record<string, unknown> {
  id: number
  created_at: number
  kind: OperationLogKind
  action: string
  params: Record<string, unknown>
  content: string
  actor: OperationLogActor
  ip: string
  user_agent: string
  request: OperationLogRequest | null
}

/* ------------------------------------------------------------------ */
/* relay task logs — drawing (Midjourney) and async platform tasks      */
/* ------------------------------------------------------------------ */

/**
 * Task status strings shared by /api/mj/self and /api/task/self.
 * The backend may introduce new states, so display mappings must keep a
 * fallback instead of the contract rejecting unknown values.
 */
export type RelayTaskStatus = string

/** One row from /api/mj/self. Timestamps are in milliseconds. */
export interface DrawingLogItem {
  id: number
  mj_id: string
  action: string
  prompt: string
  prompt_en: string
  status: RelayTaskStatus
  progress: string
  fail_reason: string
  image_url: string
  video_url: string
  quota: string
  submit_time: number
  finish_time: number
}

/** One row from /api/task/self. Timestamps are in seconds. */
export interface RelayTaskLogItem {
  id: number
  task_id: string
  platform: string
  action: string
  status: RelayTaskStatus
  progress: string
  fail_reason: string
  result_url: string
  quota: string
  submit_time: number
  finish_time: number
}

/* ---------------- model plaza (market) ---------------- */

export type MarketModelType =
  'chat' | 'image' | 'embedding' | 'rerank' | 'audio' | 'video'

export type MarketBilling =
  'token' | 'per_call' | 'tiered' | 'free' | 'unit_price'

export interface MarketPriceTier {
  label: string
  input: string
  output: string
}

export interface MarketModelPrice {
  input?: string // $ / 1M tokens
  output?: string // $ / 1M tokens
  cache_read?: string // $ / 1M tokens
  cache_write?: string // $ / 1M tokens
  per_call?: string // $ / call
  tiers?: MarketPriceTier[] // tiered billing breakdown
}

export interface MarketModel {
  id: number
  name: string
  vendor: string
  type: MarketModelType
  billing: MarketBilling
  price: MarketModelPrice
  pricing?: import('@/api/modelPricing').PublicPriceVersion
  offers?: import('@/api/liveContracts').PricingOffer[]
  context: number // context window in tokens (0 = n/a)
  tagline: string
  latency: number // seconds, snapshot
  tps: number // tokens/s, snapshot (0 = n/a)
  health: number // 0-100 → drives the health meter + tone
}

export interface InviteRecord {
  id: number
  invitee: string
  created: number
  paid: boolean
  reward_total: string
  last_paid_at: number
}

export interface InviteMonthPoint {
  month: string
  new_count: number
  cumulative: number
  reward: string
}

export interface InvitePolicy {
  enabled: boolean
  activated_at: number
  registration_required: boolean
  freeze_hours: number
  duration_days: number
  per_invitee_cap: string // exact USD decimal
  cash_withdrawal: false
}

export interface InviteInfo {
  code: string
  invited: number
  reward_total: string // lifetime reward quota earned
  transferable: string // reward quota available to move into balance
  available_reward: string
  frozen_reward: string
  total_reward: string
  effective_rate_bps: number
  effective_rate_percent: number
  code_enabled: boolean
  invite_limit: number | null
  remaining_invites: number | null
  policy: InvitePolicy
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
  message_count: number
  last_reply_role: 'user' | 'support'
  model_id?: string
  request_id?: string
  created: number
  updated: number
  messages: TicketMessage[]
}

export interface TicketAgent {
  id: number
  username: string
  display_name: string
  email?: string
}

export interface AdminTicketItem extends Omit<TicketItem, 'messages'> {
  assignee_id: number | null
  assignee_name?: string
  assigned_at: number
  user: TicketAgent
}

export interface TicketQueueSummary {
  pending: number
  unassigned: number
  mine: number
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
  reward: string
}

export interface WeekEntry {
  date: string // "07/21"
  weekday: string // "MON" | "TUE" | ...
  reward: string // quota units; 0 = not claimed
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
  total_reward: string
  month_reward: string
  best_streak: number
  week_entries: WeekEntry[]
}

export interface NewcomerTask {
  id: string
  labelKey: string
  reward: string
  done: boolean
}

export interface NewcomerPayload {
  tasks: NewcomerTask[]
  claimed: boolean
}

export interface InviteActivityPayload {
  invited: number
  reward_total: string
  available_reward: string
  frozen_reward: string
  effective_rate_percent: number
}

export type Activity = ActivityBase &
  (
    | { kind: 'checkin'; checkin: CheckinPayload }
    | { kind: 'newcomer'; newcomer: NewcomerPayload }
    | { kind: 'invite'; invite: InviteActivityPayload }
  )

export interface ActivitySummary {
  claimable: number
  reward_earned: string
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

/* ------------------------------------------------------------------ */
/* admin redemption codes                                               */
/* ------------------------------------------------------------------ */

export type AdminRedemptionType = 'quota'

/**
 * Four lifecycle states:
 *   unused   — never redeemed, within validity window
 *   used     — redeemed by exactly one user
 *   expired  — validity window has passed without being used
 *   disabled — manually deactivated by an admin
 */
export type AdminRedemptionStatus = 'unused' | 'used' | 'expired' | 'disabled'

export type AdminRedemptionSortBy = 'id' | 'created_time' | 'used_time'

export type AdminRedemptionSortOrder = 'asc' | 'desc'

export interface AdminRedemptionCode extends Record<string, unknown> {
  id: number
  name: string // display label (e.g. "$5.00", "claude", admin-set)
  code: string // raw 32-character redemption code
  type: AdminRedemptionType
  status: AdminRedemptionStatus
  quota: string
  amount: string
  // redemption record
  redeemer_id: number // 0 = not yet redeemed
  redeemer_email: string // '' = not yet redeemed
  created_time: number // unix epoch seconds
  used_time: number // 0 = not yet redeemed
  expired_time: number // -1 = never
}

export interface AdminRedemptionPage {
  items: AdminRedemptionCode[]
  total: number
  page: number
  page_size: number
  type_counts: Record<string, number>
  status_counts: Record<string, number>
}

export interface AdminRedemptionCreateInput {
  type: AdminRedemptionType
  count: number // 1–100
  amount: string
  expired_time: number // -1 = never; future unix epoch = deadline
}
