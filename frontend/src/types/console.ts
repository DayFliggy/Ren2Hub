/** Each API key independently selects automatic or manual routing. */
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
  allow_user_quota_query?: boolean
  status: number // Native: 1 enabled, 2 disabled, 3 expired, 4 exhausted
  used_quota: string
  remain_quota: number
  unlimited: boolean
  model_limits: string[]
  ip_limits: string[]
  expired_time: number // -1 = never
  created_time: number
}

export type AdminChannelStatus = 1 | 2 | 3

export type AdminChannelSortBy =
  'id' | 'name' | 'priority' | 'balance' | 'response_time'

export type AdminChannelSortOrder = 'asc' | 'desc'

/** Native model.ChannelInfo as the channel list returns it. */
export interface AdminChannelInfo {
  is_multi_key: boolean
  /** random or polling on multi-key channels; empty on single-key channels. */
  multi_key_mode: string
  multi_key_size: number
  /** Disabled keys only: key index -> 2 (manual) or 3 (automatic). */
  multi_key_status_list: Record<string, 2 | 3> | null
}

export interface AdminChannel extends Record<string, unknown> {
  channel_ratio: string
  max_concurrency: number
  in_flight: number
  lab: { id: string; name: string; source: string }
  channel_info?: AdminChannelInfo
  id: number
  name: string
  type: number
  status: AdminChannelStatus
  readonly status_reason?: string
  readonly status_time?: number
  priority: number
  weight: number
  used_quota: number
  balance: number
  response_time: number
  test_time: number
  base_url: string
  models: string
  model_mapping: string
  other_info?: string
}

export interface AdminChannelCreateInput {
  channel_ratio: string
  max_concurrency: number
  other?: string
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
  models: string
  model_mapping: string
  // Routing
  priority: number
  weight: number
}

export interface AdminChannelCreateOptions {
  batchKeys?: boolean
  multiKey?: boolean
  multiKeyMode?: 'random' | 'polling'
}

/**
 * Sensitive fields (CHANNEL_SENSITIVE_UPDATE_FIELDS) are optional: an editor
 * without ChannelSensitiveWrite omits them and the backend keeps the stored
 * values.
 */
export type AdminChannelUpdateInput = Omit<
  AdminChannelCreateInput,
  'status' | 'key' | 'type' | 'base_url' | 'channel_ratio'
> & {
  type?: number
  base_url?: string
  channel_ratio?: string
  /** Include only when replacing the stored key. */
  key?: string
}

/** Params for discovering a channel's model list from its upstream. */
export type AdminChannelFetchModelsParams =
  | { channelId: number; advancedCustom?: string; baseUrl?: string }
  | { type: number; key: string; baseUrl: string; advancedCustom?: string }

export interface AdminChannelPage {
  items: AdminChannel[]
  total: number
  page?: number
  page_size?: number
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
  | 'quota'
  | 'used_quota'
  | 'created_time'
  | 'last_login_time'

export type AdminUserSortOrder = 'asc' | 'desc'

export interface AdminUser extends Record<string, unknown> {
  aff_frozen_quota: number
  max_concurrency: number
  id: number
  username: string
  display_name: string
  email: string
  /** Upstream omits an empty remark (`omitempty`); the parser restores ''. */
  remark: string
  role: AdminUserRole
  status: AdminUserStatus
  /** From upstream `DeletedAt`: the list query is unscoped, so it includes soft-deleted users. */
  deleted: boolean
  quota: number
  used_quota: number
  request_count: number
  aff_count: number
  aff_quota: number
  aff_history_quota: number
  inviter_id: number
  created_at: number
  last_login_at: number
  admin_permissions?: Record<string, Record<string, boolean>>
}

export interface AdminUserCreateInput {
  max_concurrency: number
  admin_permissions?: Record<string, Record<string, boolean>>
  username: string
  display_name: string
  password: string
  role: AdminUserRole
}

/**
 * Status is owned by the dedicated toggle route, and email by account
 * bindings. Upstream `EditWithTx` rewrites remark on every save, so the form
 * always sends it.
 */
export type AdminUserUpdateInput = Omit<AdminUserCreateInput, 'password'> & {
  remark: string
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

export type LogType =
  'consume' | 'topup' | 'refund' | 'manage' | 'error' | 'system' | 'login'

export type LogRequestMode = 'stream' | 'sync'

export type LogOther = string | Record<string, unknown> | null

export interface LogItem {
  id: number
  /** Stable gateway request identity; absent on historical or non-request rows. */
  request_id?: string
  type: LogType
  /** 0 marks a row written without a token, such as a channel test. */
  token_id?: number
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

/** model.ValidAuditCategory: the four categories the audit filter accepts. */
export type AuditLogCategory =
  'login' | 'security' | 'operation' | 'access_token'

export interface AuditLogActor {
  id: number
  username: string
  /** null when the server recorded no valid actor role (root-only rows). */
  role: number | null
  auth_method: string
}

export interface AuditLogRequest {
  method: string
  route: string
  status: number | null
  success: boolean
}

/** One /api/audit(/self) row. The server has already applied role projection. */
export interface AuditLogItem extends Record<string, unknown> {
  event_id: string
  created_at: number
  /** Any non-empty string: rows from before the NewAPI-core rebuild may carry
   * categories outside AuditLogCategory (for example `system`). */
  category: string
  action: string
  params: Record<string, unknown>
  content: string
  actor: AuditLogActor
  token_ref: string
  request_id: string
  ip: string
  user_agent: string
  request: AuditLogRequest
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

/**
 * One row from /api/task/self. Timestamps are in seconds. Results are not
 * part of the row; they are read on demand from /api/task/:task_id/artifacts.
 */
export type RelayTaskLogItem = {
  id: number
  task_id: string
  platform: string
  action: string
  status: RelayTaskStatus
  progress: string
  fail_reason: string
  quota: string
  submit_time: number
  finish_time: number
  /** A pre-plugin video task whose result is served as a legacy artifact. */
  legacy_video_available: boolean
  /** A synchronous result returned inline and never persisted. */
  result_discarded: boolean
}

export type TaskArtifactType = 'image' | 'video' | 'audio' | 'file'

export interface TaskArtifact {
  key: string
  type: TaskArtifactType
  mime_type?: string
  /** Signed absolute URL; usable directly as a media src or download link. */
  content_url: string
}

/** One clip from a pre-plugin Suno task's persisted snapshot. */
export interface TaskAudioClip {
  audio_url: string
  clip_id?: string
  id?: string
  title?: string
  tags?: string
  duration?: number
  image_url?: string
}

export interface TaskArtifactsResult {
  task_id: string
  artifacts: TaskArtifact[]
  legacy_content_url?: string
  legacy_audio_clips?: TaskAudioClip[]
}

/* ---------------- model plaza (market) ---------------- */

export type MarketModelType =
  'chat' | 'image' | 'embedding' | 'rerank' | 'audio' | 'video'

export interface Invitee {
  masked_name: string
  invited_at: number
  reward_quota: number
}
export interface InviteRecord {
  id: number
  user_id: number
  invitee_id: number
  kind: 'accrue' | 'transfer' | 'reversal'
  skip_reason: string
  source_type: string
  source_id: string
  base_quota: string
  rate_bps: number
  reward_quota: string
  credited_quota: string
  debt_offset_quota: string
  debt_added_quota: string
  frozen_until: number
  thawed_at: number
  created_at: number
  available_after: string
  frozen_after: string
  wallet_after: string
}
export interface InviteMonthPoint {
  month: string
  reward: number
  quota: number
}
export interface InvitePolicy {
  enabled: boolean
  rate_bps: number
  freeze_hours: number
  duration_days: number
  per_invitee_cap_quota: number
  admin_recharge_enabled: boolean
}
export interface InviteInfo {
  code: string
  code_enabled: boolean
  invited: number
  effective_rate_bps: number
  effective_rate_percent: number
  reward_total: number
  transferable: number
  available_reward: number
  frozen_reward: number
  total_reward: number
  policy: InvitePolicy
  monthly_series: InviteMonthPoint[]
  records: InviteRecord[]
}
export interface AdminAffiliateUser {
  user_id: number
  code: string
  code_custom: boolean
  enabled: boolean
  rate_bps: number | null
  effective_rate_bps: number
  inviter_id: number
  invited_at: number
}
export interface AdminAffiliateUserUpdateInput {
  rate_bps: number | null
  code?: string
  reset_code?: boolean
  enabled?: boolean
}

/* ---------------- tickets (support ledger) ---------------- */

export type TicketStatus = 'open' | 'replied' | 'closed'

export type TicketCategory = 'billing' | 'api' | 'model' | 'account' | 'other'

export type TicketPriority = 'low' | 'normal' | 'high'

export type TicketRole = 'user' | 'support'

export interface TicketMessage {
  id: number
  role: TicketRole
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
  // A soft-deleted requester keeps its name; a hard-deleted one only its id.
  user: TicketAgent
  user_deleted: boolean
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
  name: string
  type: AdminRedemptionType
  count: number // 1–100
  amount: string
  expired_time: number // -1 = never; future unix epoch = deadline
}

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

export type AdminOrderCurrency = string

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

/** One payment provider (or legacy payment_method) within one currency. */
export interface AdminOrderMethodShare {
  method: string
  amount: number
  count: number
}

/** Ranked by credited quota, which is comparable across currencies. */
export interface AdminOrderSpender {
  user_id: number
  username: string
  paid_count: number
  credited_quota: string
}

export interface AdminOrderCurrencyStats {
  currency: string
  count: number
  paid_count: number
  paid_amount: string
  credited_quota: number
  unknown_amount_count: number
}
export interface AdminOrderStats {
  total: number
  currencies: AdminOrderCurrencyStats[]
  methods: (AdminOrderCurrencyStats & { method: string })[]
  daily: (AdminOrderCurrencyStats & { date: string })[]
  start_time: number
  end_time: number
}

export type AdminOrderStatus = 'pending' | 'completed' | 'failed' | 'expired'
