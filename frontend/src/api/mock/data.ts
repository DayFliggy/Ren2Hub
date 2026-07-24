import { marketSources, marketTagPool } from '@/constants/console'
import type { UserInfo } from '@/types/auth'
import type {
  TokenType,
  TokenChannel,
  TokenItem,
  LogType,
  LogItem,
  TopupRecord,
  Plan,
  MarketModelType,
  MarketModel,
  InviteRecordStatus,
  InviteInfo,
  TicketItem,
  Activity,
  ActivitySummary,
  MerchantScale,
  ListingStatus,
  CurrentSubscription,
  MerchantComment,
  Merchant,
  MarketListing,
  MyChannel,
  InvoiceItem,
} from '@/types/console'

export const QUOTA_PER_DOLLAR = 500_000

export const GROUPS = ['default', 'vip', 'svip']

const marketRaw: Omit<MarketModel, 'id'>[] = [
  /* ---- OpenAI ---- */
  {
    name: 'gpt-4o',
    vendor: 'OpenAI',
    type: 'chat',
    billing: 'tiered',
    price: {
      input: 2.5,
      output: 10,
      cache_read: 1.25,
      tiers: [
        { label: '标准', input: 2.5, output: 10 },
        { label: '批量', input: 1.25, output: 5 },
      ],
    },
    context: 128_000,
    tagline: '全能旗舰多模态模型，均衡的速度与质量，支持文本 / 图像输入。',
    latency: 1.78,
    tps: 62.4,
    health: 98,
    channels: ['OpenAI 官方', 'Azure 美东', 'Azure 欧西'],
  },
  {
    name: 'gpt-4o-mini',
    vendor: 'OpenAI',
    type: 'chat',
    billing: 'token',
    price: { input: 0.15, output: 0.6, cache_read: 0.075 },
    context: 128_000,
    tagline: '轻量高速版本，适合高并发与低成本场景，性价比极高。',
    latency: 0.92,
    tps: 112.5,
    health: 99,
    channels: ['OpenAI 官方', 'Azure 美东'],
  },
  {
    name: 'o3',
    vendor: 'OpenAI',
    type: 'chat',
    billing: 'token',
    price: { input: 2, output: 8, cache_read: 0.5 },
    context: 200_000,
    tagline: '深度推理模型，擅长复杂数理、代码与多步规划，响应偏慢。',
    latency: 12.4,
    tps: 38.1,
    health: 95,
    channels: ['OpenAI 官方'],
  },
  {
    name: 'gpt-image-1',
    vendor: 'OpenAI',
    type: 'image',
    billing: 'per_call',
    price: { per_call: 0.04 },
    context: 0,
    tagline: '高质量图像生成，支持透明背景与精确文字渲染，按张计费。',
    latency: 8.2,
    tps: 0,
    health: 92,
    channels: ['OpenAI 官方', 'Azure 美东'],
  },
  {
    name: 'text-embedding-3-large',
    vendor: 'OpenAI',
    type: 'embedding',
    billing: 'token',
    price: { input: 0.13 },
    context: 8_191,
    tagline: '高维语义向量模型，适合检索增强（RAG）与相似度匹配。',
    latency: 0.31,
    tps: 0,
    health: 99,
    channels: ['OpenAI 官方'],
  },
  /* ---- Anthropic ---- */
  {
    name: 'claude-sonnet-4.5',
    vendor: 'Anthropic',
    type: 'chat',
    billing: 'token',
    price: { input: 3, output: 15, cache_read: 0.3 },
    context: 200_000,
    tagline: '编码与 Agent 场景的当红主力，长上下文稳定，工具调用可靠。',
    latency: 2.1,
    tps: 55.3,
    health: 99,
    channels: ['Anthropic 官方', 'AWS Bedrock', 'GCP Vertex'],
  },
  {
    name: 'claude-opus-4.1',
    vendor: 'Anthropic',
    type: 'chat',
    billing: 'tiered',
    price: {
      input: 15,
      output: 75,
      cache_read: 1.5,
      tiers: [
        { label: '标准', input: 15, output: 75 },
        { label: '批量', input: 7.5, output: 37.5 },
      ],
    },
    context: 200_000,
    tagline: '最强推理旗舰，复杂任务质量优先，成本较高、响应偏慢。',
    latency: 4.6,
    tps: 28.4,
    health: 97,
    channels: ['Anthropic 官方', 'AWS Bedrock'],
  },
  {
    name: 'claude-haiku-4.5',
    vendor: 'Anthropic',
    type: 'chat',
    billing: 'token',
    price: { input: 1, output: 5, cache_read: 0.1 },
    context: 200_000,
    tagline: '轻量高速版本，延迟低、吞吐高，适合实时对话与批量处理。',
    latency: 1.1,
    tps: 88.6,
    health: 98,
    channels: ['Anthropic 官方', 'AWS Bedrock'],
  },
  /* ---- Google ---- */
  {
    name: 'gemini-2.5-pro',
    vendor: 'Google',
    type: 'chat',
    billing: 'tiered',
    price: {
      input: 1.25,
      output: 10,
      cache_read: 0.31,
      tiers: [
        { label: '≤200K', input: 1.25, output: 10 },
        { label: '>200K', input: 2.5, output: 15 },
      ],
    },
    context: 1_000_000,
    tagline: '超长上下文旗舰，1M token 窗口，多模态理解强，价格随长度分档。',
    latency: 3.4,
    tps: 48.7,
    health: 96,
    channels: ['GCP Vertex', 'Google AI Studio'],
  },
  {
    name: 'gemini-2.5-flash',
    vendor: 'Google',
    type: 'chat',
    billing: 'token',
    price: { input: 0.3, output: 2.5, cache_read: 0.075 },
    context: 1_000_000,
    tagline: '高速轻量多模态，长上下文与低成本兼顾，适合规模化调用。',
    latency: 1.3,
    tps: 96.2,
    health: 98,
    channels: ['GCP Vertex', 'Google AI Studio'],
  },
  {
    name: 'imagen-4',
    vendor: 'Google',
    type: 'image',
    billing: 'per_call',
    price: { per_call: 0.04 },
    context: 0,
    tagline: '写实级图像生成，细节与构图出色，按张计费。',
    latency: 6.8,
    tps: 0,
    health: 90,
    channels: ['GCP Vertex'],
  },
  /* ---- DeepSeek ---- */
  {
    name: 'deepseek-v3.2',
    vendor: 'DeepSeek',
    type: 'chat',
    billing: 'token',
    price: { input: 0.28, output: 0.42, cache_read: 0.028 },
    context: 128_000,
    tagline: '开源高性价比通用模型，中文表现优异，缓存命中价格极低。',
    latency: 2.7,
    tps: 42.3,
    health: 94,
    channels: ['DeepSeek 官方', '硅基流动'],
  },
  {
    name: 'deepseek-r1',
    vendor: 'DeepSeek',
    type: 'chat',
    billing: 'token',
    price: { input: 0.55, output: 2.19, cache_read: 0.14 },
    context: 128_000,
    tagline: '开源推理模型，思维链透明，数理与代码能力突出。',
    latency: 9.5,
    tps: 33.8,
    health: 91,
    channels: ['DeepSeek 官方', '硅基流动', '火山引擎'],
  },
  /* ---- 阿里通义 ---- */
  {
    name: 'qwen3-max',
    vendor: '阿里通义',
    type: 'chat',
    billing: 'token',
    price: { input: 1.2, output: 6, cache_read: 0.24 },
    context: 256_000,
    tagline: '通义千问旗舰，综合能力强，中英文与工具调用均衡。',
    latency: 2.9,
    tps: 51.2,
    health: 96,
    channels: ['阿里云百炼', '硅基流动'],
  },
  {
    name: 'qwen3-vl-plus',
    vendor: '阿里通义',
    type: 'chat',
    billing: 'token',
    price: { input: 0.8, output: 3.2 },
    context: 128_000,
    tagline: '视觉语言多模态，图文理解与文档解析能力强。',
    latency: 3.6,
    tps: 44.1,
    health: 93,
    channels: ['阿里云百炼'],
  },
  {
    name: 'text-embedding-v4',
    vendor: '阿里通义',
    type: 'embedding',
    billing: 'token',
    price: { input: 0.07 },
    context: 8_192,
    tagline: '通用文本向量模型，多语言检索友好，成本低廉。',
    latency: 0.28,
    tps: 0,
    health: 97,
    channels: ['阿里云百炼'],
  },
  /* ---- xAI ---- */
  {
    name: 'grok-4',
    vendor: 'xAI',
    type: 'chat',
    billing: 'token',
    price: { input: 3, output: 15, cache_read: 0.75 },
    context: 256_000,
    tagline: '实时联网大模型，接入 X 数据，时效性问题回答见长。',
    latency: 3.8,
    tps: 46.9,
    health: 89,
    channels: ['xAI 官方'],
  },
  {
    name: 'grok-4-fast',
    vendor: 'xAI',
    type: 'chat',
    billing: 'token',
    price: { input: 0.2, output: 0.5 },
    context: 2_000_000,
    tagline: '超长上下文高速版，2M 窗口，适合大文档与低延迟场景。',
    latency: 1.6,
    tps: 78.4,
    health: 85,
    channels: ['xAI 官方'],
  },
  /* ---- Moonshot ---- */
  {
    name: 'kimi-k2.5',
    vendor: 'Moonshot',
    type: 'chat',
    billing: 'token',
    price: { input: 0.6, output: 2.5, cache_read: 0.15 },
    context: 256_000,
    tagline: '长文与 Agent 能力突出，工具调用稳定，中文写作出色。',
    latency: 2.4,
    tps: 49.8,
    health: 82,
    channels: ['Moonshot 官方', '硅基流动'],
  },
  /* ---- 智谱AI ---- */
  {
    name: 'glm-4.6',
    vendor: '智谱AI',
    type: 'chat',
    billing: 'token',
    price: { input: 0.6, output: 2.2, cache_read: 0.11 },
    context: 200_000,
    tagline: 'GLM 系列旗舰，代码与推理均衡，国产合规首选之一。',
    latency: 2.6,
    tps: 47.5,
    health: 76,
    channels: ['智谱开放平台', '硅基流动'],
  },
  {
    name: 'cogview-4',
    vendor: '智谱AI',
    type: 'image',
    billing: 'per_call',
    price: { per_call: 0.03 },
    context: 0,
    tagline: '中文语义友好的图像生成，支持中文提示词与文字渲染。',
    latency: 7.4,
    tps: 0,
    health: 64,
    channels: ['智谱开放平台'],
  },
  {
    name: 'glm-4-voice',
    vendor: '智谱AI',
    type: 'audio',
    billing: 'per_call',
    price: { per_call: 0.02 },
    context: 0,
    tagline: '端到端语音模型，支持情感语调与实时对话，按次计费。',
    latency: 1.9,
    tps: 0,
    health: 58,
    channels: ['智谱开放平台'],
  },
]

export const marketModels: MarketModel[] = marketRaw.map((m, i) => ({
  ...m,
  id: i + 1,
}))

export const marketChannels: string[] = [
  ...new Set(marketModels.flatMap((m) => m.channels)),
]

export const marketVendors: string[] = [
  ...new Set(marketModels.map((m) => m.vendor)),
]

/** name → AI vendor lookup, used to populate listing.modelVendors at seed time. */
export const modelVendorMap: Record<string, string> = Object.fromEntries(
  marketModels.map((m) => [m.name, m.vendor])
)

/* ---------- seeded pseudo-random for stable demo data ---------- */
function mulberry32(seed: number) {
  return () => {
    seed |= 0
    seed = (seed + 0x6d2b79f5) | 0
    let t = Math.imul(seed ^ (seed >>> 15), 1 | seed)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}
const rand = mulberry32(20260717)

const now = Math.floor(Date.now() / 1000)
const DAY = 86_400

export const mockUser: UserInfo = {
  id: 1,
  username: 'bigd.studio',
  display_name: 'Bigd.Studio',
  email: 'hello@bigd.studio',
  role: 1,
  quota: 5_201_314,
  used_quota: 2_985_211,
  group: 'vip',
}

function randomKey() {
  const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789'
  let out = 'sk-'
  for (let i = 0; i < 40; i++) out += chars[Math.floor(rand() * chars.length)]
  return out
}

const ch = (name: string, enabled = true, weight?: number): TokenChannel =>
  weight === undefined ? { name, enabled } : { name, enabled, weight }

export const tokens: TokenItem[] = (
  [
    // name, type, status, group, model_limits, remain, unlimited, rate_limit, max_ratio, load_balance, channels
    [
      '生产环境主 Key',
      'auto',
      1,
      'default',
      [],
      9_999_999,
      true,
      0,
      1.5,
      false,
      [],
    ],
    [
      'Chatbox 客户端',
      'platform',
      1,
      'default',
      ['gpt-4o', 'gpt-4o-mini'],
      500_000,
      false,
      60,
      undefined,
      false,
      [ch('OpenAI 官方'), ch('Azure 美东')],
    ],
    [
      'NextChat 网页端',
      'platform',
      1,
      'vip',
      [],
      2_000_000,
      false,
      0,
      undefined,
      true,
      [
        ch('OpenAI 官方', true, 2),
        ch('Azure 美东', true, 1),
        ch('Azure 欧西', false, 1),
      ],
    ],
    [
      'CI 自动化测试',
      'platform',
      2,
      'default',
      ['deepseek-v3.2'],
      300_000,
      false,
      120,
      undefined,
      false,
      [ch('DeepSeek 官方'), ch('硅基流动')],
    ],
    [
      'Claude Code 专用',
      'market',
      1,
      'vip',
      ['claude-sonnet-4.5', 'claude-opus-4.1'],
      5_000_000,
      false,
      0,
      2,
      false,
      [ch('星链 API'), ch('砺石智汇', false)],
    ],
    ['临时调试', 'auto', 2, 'default', [], 100_000, false, 30, 1.2, false, []],
    [
      '团队共享',
      'market',
      1,
      'svip',
      [],
      20_000_000,
      false,
      0,
      3,
      true,
      [
        ch('云枢智算', true, 3),
        ch('方舟接入', true, 2),
        ch('流沙聚合', true, 1),
      ],
    ],
    [
      'LobeChat',
      'platform',
      1,
      'default',
      ['gpt-4o', 'gemini-2.5-pro'],
      800_000,
      false,
      0,
      undefined,
      false,
      [ch('OpenAI 官方'), ch('GCP Vertex')],
    ],
  ] as Array<
    [
      string,
      TokenType,
      1 | 2,
      string,
      string[],
      number,
      boolean,
      number,
      number | undefined,
      boolean,
      TokenChannel[],
    ]
  >
).map(
  (
    [
      name,
      type,
      status,
      group,
      limits,
      remain,
      unlimited,
      rateLimit,
      maxRatio,
      loadBalance,
      channels,
    ],
    i
  ) => ({
    id: i + 1,
    name,
    key: randomKey(),
    type,
    status,
    used_quota: Math.floor(rand() * 900_000),
    remain_quota: remain,
    unlimited,
    group,
    model_limits: limits,
    ip_limits: i === 0 ? ['120.244.0.0/16'] : [],
    rate_limit: rateLimit,
    max_ratio: maxRatio,
    load_balance: loadBalance,
    channels,
    expired_time: i === 3 ? now + 30 * DAY : -1,
    created_time: now - Math.floor(rand() * 220) * DAY,
  })
)

const logModels = [
  'gpt-4o',
  'claude-sonnet-4.5',
  'gemini-2.5-pro',
  'deepseek-v3.2',
  'gpt-4o-mini',
  'kimi-k2.5',
  'qwen3-max',
  'grok-4',
]

const logChannels: Record<string, string> = {
  'gpt-4o': 'OpenAI 官方',
  'gpt-4o-mini': 'Azure 美东',
  'claude-sonnet-4.5': 'Anthropic 官方',
  'gemini-2.5-pro': 'GCP Vertex',
  'deepseek-v3.2': '硅基流动',
  'kimi-k2.5': 'Moonshot 官方',
  'qwen3-max': '阿里云百炼',
  'grok-4': 'xAI 官方',
}

// Six log types distributed across 67 rows for realistic demo data
const LOG_TYPES: LogType[] = [
  'consume',
  'consume',
  'consume',
  'consume',
  'topup',
  'refund',
  'manage',
  'error',
  'system',
  'consume',
  'consume',
]

const logTokenNames = [
  '生产环境主 Key',
  'Chatbox 客户端',
  'NextChat 网页端',
  'Claude Code 专用',
  'LobeChat',
  '团队共享',
]

export const logs: LogItem[] = Array.from({ length: 67 }, (_, i) => {
  const type: LogType = LOG_TYPES[i % LOG_TYPES.length]
  const isConsume = type === 'consume'
  const isError = type === 'error'
  const model =
    isConsume || isError
      ? logModels[Math.floor(rand() * logModels.length)]
      : '—'
  const channel =
    isConsume || isError ? (logChannels[model] ?? 'OpenAI 官方') : '—'
  const tokenName =
    isConsume || isError
      ? logTokenNames[Math.floor(rand() * logTokenNames.length)]
      : '—'
  const prompt = isConsume || isError ? Math.floor(rand() * 6000) + 200 : 0
  const completion = isConsume ? Math.floor(rand() * 2000) + 50 : 0
  // latency: consume 0.5–12s, error 15–30s (timeout), others 0
  const latency = isConsume
    ? Math.round((0.5 + rand() * 11.5) * 100) / 100
    : isError
      ? Math.round((15 + rand() * 15) * 100) / 100
      : 0
  // tps: only meaningful for consume (non-zero completion)
  const tps = isConsume && latency > 0 ? Math.round(completion / latency) : 0

  const contentMap: Record<LogType, string> = {
    consume: '调用成功',
    topup: '在线充值到账',
    refund: '退款已处理',
    manage: '管理员调整额度',
    error: '上游超时，未计费',
    system: '系统赠送额度',
  }

  return {
    id: 9000 - i,
    type,
    token_name: tokenName,
    model,
    channel,
    prompt_tokens: prompt,
    completion_tokens: completion,
    quota:
      type === 'topup'
        ? 2_500_000
        : type === 'refund'
          ? Math.floor(rand() * 500_000) + 100_000
          : type === 'manage'
            ? Math.floor(rand() * 1_000_000) + 200_000
            : isConsume
              ? Math.floor((prompt + completion * 3) * (0.8 + rand()))
              : 0,
    latency,
    tps,
    content: contentMap[type],
    created: now - Math.floor(i * 0.7 * DAY) - Math.floor(rand() * DAY),
  }
}).sort((a, b) => b.created - a.created)

export const topupRecords: TopupRecord[] = Array.from(
  { length: 12 },
  (_, i) => {
    const method = (['epay', 'stripe', 'creem', 'redeem'] as const)[i % 4]
    const amount = [5, 10, 20, 50][Math.floor(rand() * 4)]
    return {
      id: 700 - i,
      trade_no: `T${20260}${String(5100 + i * 37)}${String(100000 + Math.floor(rand() * 899999))}`,
      amount,
      money: amount * QUOTA_PER_DOLLAR,
      method,
      status: i === 2 ? 'pending' : i === 7 ? 'failed' : 'success',
      created: now - i * 6 * DAY - Math.floor(rand() * DAY),
    }
  }
)

export const plans: Plan[] = [
  {
    id: 1,
    name: '轻量版',
    price: 5,
    quota: 10_000_000,
    duration_days: 30,
    features: ['全部公开模型', 'default 分组', '社区支持'],
    gradient: 'signal',
  },
  {
    id: 2,
    name: '专业版',
    price: 20,
    quota: 60_000_000,
    duration_days: 30,
    features: ['全部公开模型', 'vip 分组高速通道', '优先客服', '用量分析报表'],
    gradient: 'accent',
    recommended: true,
  },
  {
    id: 3,
    name: '团队版',
    price: 80,
    quota: 300_000_000,
    duration_days: 30,
    features: ['svip 专属通道', '多成员协作', '专属客户经理', '发票与合同'],
    gradient: 'support',
  },
]

export const currentSubscription: CurrentSubscription = {
  plan_id: 2,
  name: '专业版',
  total_quota: 60_000_000,
  remain_quota: 41_773_482,
  expire_time: now + 18 * DAY,
  auto_renew: true,
}

export const inviteInfo: InviteInfo = {
  code: 'BIGD2026',
  invited: 23,
  rate: 0.02,
  reward_total: 4_600_000,
  transferable: 1_150_000,
  pending_reward: 0,
  qualification: {
    token_used: mockUser.used_quota, // 2.98M, past the threshold below
    token_required: 2_000_000, // spend threshold to unlock referral
    topup_total: 260 * QUOTA_PER_DOLLAR,
    topup_required: 5 * QUOTA_PER_DOLLAR, // $5 minimum top-up
    qualified: true, // both thresholds met → eligible
  },
  unlock_channels: [
    { id: 'vip', name: 'vip 高速通道', detail: '', unlocked: true },
    { id: 'gptpro', name: 'GPTPRO', detail: '×0.03 计费', unlocked: true },
    {
      id: 'domestic',
      name: '国产特惠分组',
      detail: '×0.1 计费',
      unlocked: true,
    },
  ],
  monthly_series: (() => {
    let cumulative = 0
    return Array.from({ length: 6 }, (_, i) => {
      const newCount = i === 4 ? 1 : Math.floor(rand() * 4)
      cumulative += newCount
      const d = new Date((now - (5 - i) * 30 * DAY) * 1000)
      return { month: `${d.getMonth() + 1}月`, new_count: newCount, cumulative }
    })
  })(),
  records: Array.from({ length: 8 }, (_, i) => ({
    id: i + 1,
    invitee: `user_${1000 + i * 7}`,
    reward: i === 7 ? 0 : 200_000,
    status: (i === 7 ? 'pending' : 'valid') as InviteRecordStatus,
    created: now - i * 4 * DAY - Math.floor(rand() * DAY),
  })),
}

/* Dashboard 30-day series + model share */
export const flowSeries = Array.from({ length: 30 }, (_, i) => {
  const consume = Math.floor(60_000 + rand() * 240_000)
  const requests = Math.floor(180 + rand() * 900)
  return {
    date: new Date((now - (29 - i) * DAY) * 1000).toISOString().slice(5, 10),
    consume,
    requests,
    topup: i === 12 || i === 26 ? 10_000_000 : 0,
  }
})

export const modelShare = [
  { model: 'claude-sonnet-4.5', ratio: 47.8, quota: 1_426_392 },
  { model: 'gpt-4o', ratio: 25.2, quota: 752_213 },
  { model: 'deepseek-v3.2', ratio: 15.5, quota: 462_708 },
  { model: '其他', ratio: 14.8, quota: 441_612 },
  { model: 'gemini-2.5-pro', ratio: -3.3, quota: 0 }, // normalized away below
].filter((m) => m.ratio > 0)
const shareTotal = modelShare.reduce((s, m) => s + m.ratio, 0)
modelShare.forEach((m) => {
  m.ratio = Math.round((m.ratio / shareTotal) * 1000) / 10
})

export const dashboardStats = {
  quota: mockUser.quota,
  used_quota: mockUser.used_quota,
  today_quota: 96_402,
  today_requests: 312,
  total_requests: 18_764,
  month_quota_delta: 16.2,
  month_requests_delta: -5.8,
}

export const tickets: TicketItem[] = [
  {
    id: 1,
    title: 'API 调用返回 429 错误',
    category: 'api',
    priority: 'high',
    status: 'replied',
    reply_count: 3,
    last_reply_role: 'support',
    request_id: 'req_abc123xyz',
    created: now - 2 * DAY,
    updated: now - 3600,
    messages: [
      {
        id: 1,
        role: 'user',
        content:
          '使用 gpt-4 模型频繁收到 429 错误，请求 ID 为 req_abc123xyz，已经等待 10 分钟仍然无法恢复。',
        images: [],
        created: now - 2 * DAY,
      },
      {
        id: 2,
        role: 'support',
        department: 'tech',
        content:
          '您好，已确认该时段上游 OpenAI 出现限流。我们已切换到备用渠道，请重试。同时为您的账户增加了 10 元补偿额度。',
        images: [],
        created: now - 2 * DAY + 7200,
      },
      {
        id: 3,
        role: 'user',
        content: '已恢复正常，感谢处理！',
        images: [],
        created: now - 3600,
      },
    ],
  },
  {
    id: 2,
    title: '账单明细导出功能异常',
    category: 'billing',
    priority: 'normal',
    status: 'replied',
    reply_count: 2,
    last_reply_role: 'support',
    created: now - 5 * 3600,
    updated: now - 2 * 3600,
    messages: [
      {
        id: 4,
        role: 'user',
        content: '导出 7 月份账单时页面一直转圈，无法下载 CSV 文件。',
        images: [],
        created: now - 5 * 3600,
      },
      {
        id: 8,
        role: 'support',
        department: 'finance',
        content:
          '您好，已核对您的账单数据。7 月账单较大，导出耗时略长，现已为您在后台生成并发送至账户邮箱，请注意查收。',
        images: [],
        created: now - 2 * 3600,
      },
    ],
  },
  {
    id: 3,
    title: 'Claude 模型响应速度慢',
    category: 'model',
    priority: 'low',
    status: 'closed',
    reply_count: 3,
    last_reply_role: 'support',
    model_id: 'claude-3-opus',
    created: now - 7 * DAY,
    updated: now - 6 * DAY,
    messages: [
      {
        id: 5,
        role: 'user',
        content: 'claude-3-opus 模型最近几天响应时间超过 30 秒，其他模型正常。',
        images: [],
        created: now - 7 * DAY,
      },
      {
        id: 6,
        role: 'support',
        department: 'tech',
        content:
          'Anthropic 官方近期负载较高，我们已增加备用通道。当前平均延迟已降至 8 秒以内。',
        images: [],
        created: now - 6.5 * DAY,
      },
      {
        id: 7,
        role: 'system',
        content: '工单已关闭',
        images: [],
        created: now - 6 * DAY,
      },
    ],
  },
]

const activityNow = Math.floor(Date.now() / 1000)
const activityDay = 86_400

export const activities: Activity[] = [
  {
    id: 1,
    kind: 'checkin',
    title: '每日签到',
    tagline: '连续签到 7 天，奖励阶梯递增',
    status: 'ongoing',
    gradient: 'accent',
    badgeKey: 'hot',
    start: activityNow - 3 * activityDay,
    end: activityNow + 27 * activityDay,
    icon: 'M8 2v4M16 2v4M3 8h18M5 4h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2Z',
    checkin: {
      days: Array.from({ length: 7 }, (_, i) => ({
        done: i < 3,
        reward: 10_000 * (i + 1),
      })),
      todayClaimed: false,
      streak: 3,
      total_days: 45,
      month_days: 18,
      month_days_total: 31,
      total_reward: 1_260_000,
      month_reward: 420_000,
      best_streak: 14,
      // Week of Mon Jul 20 – Sun Jul 26, 2026 (today = Thu Jul 23)
      week_entries: [
        {
          date: '07/20',
          weekday: 'MON',
          reward: 0,
          claimed: false,
          today: false,
        },
        {
          date: '07/21',
          weekday: 'TUE',
          reward: 10_000,
          claimed: true,
          today: false,
        },
        {
          date: '07/22',
          weekday: 'WED',
          reward: 20_000,
          claimed: true,
          today: false,
        },
        {
          date: '07/23',
          weekday: 'THU',
          reward: 30_000,
          claimed: false,
          today: true,
        },
        {
          date: '07/24',
          weekday: 'FRI',
          reward: 0,
          claimed: false,
          today: false,
        },
        {
          date: '07/25',
          weekday: 'SAT',
          reward: 0,
          claimed: false,
          today: false,
        },
        {
          date: '07/26',
          weekday: 'SUN',
          reward: 0,
          claimed: false,
          today: false,
        },
      ],
    },
  },
  {
    id: 2,
    kind: 'newcomer',
    title: '新人礼包',
    tagline: '完成新手任务，一键领取全部奖励',
    status: 'ongoing',
    gradient: 'signal',
    badgeKey: 'new',
    start: activityNow - 10 * activityDay,
    end: activityNow + 60 * activityDay,
    icon: 'M20 12v10H4V12M2 7h20v5H2zM12 22V7M12 7H7.5a2.5 2.5 0 0 1 0-5C11 2 12 7 12 7ZM12 7h4.5a2.5 2.5 0 0 0 0-5C13 2 12 7 12 7Z',
    newcomer: {
      tasks: [
        {
          id: 'first-key',
          labelKey: 'activity.newcomer.taskFirstKey',
          reward: 20_000,
          done: true,
        },
        {
          id: 'first-call',
          labelKey: 'activity.newcomer.taskFirstCall',
          reward: 30_000,
          done: true,
        },
        {
          id: 'profile',
          labelKey: 'activity.newcomer.taskProfile',
          reward: 10_000,
          done: false,
        },
        {
          id: 'topup',
          labelKey: 'activity.newcomer.taskTopup',
          reward: 50_000,
          done: false,
        },
      ],
      claimed: false,
    },
  },
  {
    id: 4,
    kind: 'invite',
    title: '邀请返利',
    tagline: '邀请好友，共享消费返利',
    status: 'ongoing',
    gradient: 'signal',
    start: activityNow - 90 * activityDay,
    end: activityNow + 365 * activityDay,
    icon: 'M16 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0ZM19 8v6M22 11h-6',
    invite: {
      invited: inviteInfo.invited,
      reward_total: inviteInfo.reward_total,
      rate: inviteInfo.rate,
    },
  },
]

export const activitySummary: ActivitySummary = {
  claimable: 2,
  reward_earned: 4_600_000,
  ongoing: activities.filter((a) => a.status === 'ongoing').length,
}
const merchantSeed: Array<[string, MerchantScale, boolean]> = [
  // name, scale, verified
  ['云枢智算', 'empire', true],
  ['星链 API', 'empire', true],
  ['极光中转', 'studio', true],
  ['海豚算力', 'studio', true],
  ['方舟接入', 'empire', true],
  ['清水湾节点', 'workshop', false],
  ['磐石供应', 'studio', true],
  ['长风渠道', 'vendor', false],
  ['光年直连', 'studio', true],
  ['砺石智汇', 'empire', true],
  ['青鸟中枢', 'workshop', false],
  ['流沙聚合', 'studio', true],
  ['寒山工作室', 'workshop', false],
  ['银弧算力', 'studio', true],
  ['子夜供货', 'vendor', false],
  ['鲲鹏接入', 'empire', true],
  ['溪源节点', 'workshop', false],
  ['天工中转', 'studio', true],
]

/** Official first-party merchant; listed first so its group renders on top. */
const PLATFORM_MERCHANT_ID = 19

const commentUserPool = [
  '青柠',
  '老白',
  'Neo',
  '阿蛮',
  '数羊人',
  'Kova',
  '临江仙',
  '半糖',
  '仲夏',
  'Riven',
  '拾荒客',
  '苏打绿茶',
  '牧云',
  'Echo',
  '一只柯基',
]

const commentTextPool = [
  '接入两周了，高峰期也没掉过链子，稳。',
  '延迟比官方直连还低一点，不知道怎么做到的。',
  '客服响应很快，半夜提工单十分钟就回了。',
  '价格是真便宜，但偶尔会限流，轻量场景够用。',
  '跑批任务连续三天没断流，值得推荐。',
  '流式输出偶发卡顿，反馈后第二天就修复了。',
  '模型列表更新很及时，新模型上得快。',
  '用了一个月，账单清晰透明，没有暗坑。',
  '并发上到 200 也没报错，压测数据可信。',
  '有一次故障切换生效了，几乎无感知。',
  '文档和示例齐全，接入成本很低。',
  '客单小但服务态度好，适合个人开发者。',
  '晚高峰速度会降一些，介意的慎选。',
  '发票流程顺畅，公司报销无压力。',
  '换过三家中转，最后留下来的就这家。',
]

let nextCommentId = 1
/** Deterministically pick 2-5 comments for a merchant, newest first. */
function seedComments(): MerchantComment[] {
  const count = 2 + Math.floor(rand() * 4)
  const used = new Set<number>()
  const out: MerchantComment[] = []
  for (let i = 0; i < count; i++) {
    let idx = Math.floor(rand() * commentTextPool.length)
    while (used.has(idx)) idx = (idx + 1) % commentTextPool.length
    used.add(idx)
    out.push({
      id: nextCommentId++,
      user: commentUserPool[Math.floor(rand() * commentUserPool.length)],
      content: commentTextPool[idx],
      createdAt: now - Math.floor(rand() * 90) * DAY - Math.floor(rand() * DAY),
    })
  }
  return out.sort((a, b) => b.createdAt - a.createdAt)
}

export const marketMerchants: Merchant[] = [
  {
    id: PLATFORM_MERCHANT_ID,
    name: '人人平台',
    scale: 'platform',
    comments: seedComments(),
    verified: true,
    channelCount: 0,
    joinedAt: now - 1200 * DAY,
  },
  ...merchantSeed.map(([name, scale, verified], i): Merchant => ({
    id: i + 1,
    name,
    scale,
    comments: seedComments(),
    verified,
    // Filled after listings are generated (distinct sources per merchant).
    channelCount: 0,
    joinedAt: now - Math.floor(120 + rand() * 900) * DAY,
  })),
]

/**
 * Base offers seeded across merchants. Each references a real model name from
 * MODELS/marketRaw so the supported-models preview reads as production data.
 * priceUSD is intentionally slightly cheaper/dearer than the plaza list price
 * to make comparison meaningful.
 */
const listingSeed: Array<{
  merchantId: number
  title: string
  summary: string
  models: string[]
  type: MarketModelType
  priceUSD: number
}> = [
  {
    merchantId: 1,
    title: 'GPT-4o 高速供应',
    summary: '官方直连 + Azure 双通道热备，故障秒级切换。',
    models: ['gpt-4o', 'gpt-4o-mini'],
    type: 'chat',
    priceUSD: 2.3,
  },
  {
    merchantId: 1,
    title: 'o3 推理专线',
    summary: '深度推理模型独立限流池，长任务不打断。',
    models: ['o3'],
    type: 'chat',
    priceUSD: 1.9,
  },
  {
    merchantId: 2,
    title: 'Claude 4.5 全家桶',
    summary: 'Sonnet / Opus / Haiku 统一入口，长上下文稳定。',
    models: ['claude-sonnet-4.5', 'claude-opus-4.1', 'claude-haiku-4.5'],
    type: 'chat',
    priceUSD: 2.9,
  },
  {
    merchantId: 2,
    title: 'Claude Haiku 极速供应',
    summary: '低延迟高吞吐，适合实时对话与批处理。',
    models: ['claude-haiku-4.5'],
    type: 'chat',
    priceUSD: 0.95,
  },
  {
    merchantId: 3,
    title: 'Gemini 2.5 Pro 长文供应',
    summary: '1M token 窗口，多模态理解，价格随长度分档。',
    models: ['gemini-2.5-pro', 'gemini-2.5-flash'],
    type: 'chat',
    priceUSD: 1.2,
  },
  {
    merchantId: 4,
    title: 'DeepSeek 高性价比池',
    summary: '国产开源旗舰，缓存命中价格极低，中文优异。',
    models: ['deepseek-v3.2', 'deepseek-r1'],
    type: 'chat',
    priceUSD: 0.26,
  },
  {
    merchantId: 5,
    title: 'GPT-4o 企业级供应',
    summary: '企业 SLA 保障，独享配额，发票与合同齐全。',
    models: ['gpt-4o'],
    type: 'chat',
    priceUSD: 2.5,
  },
  {
    merchantId: 5,
    title: 'gpt-image-1 出图供应',
    summary: '高质量图像生成，支持透明背景，按张计费。',
    models: ['gpt-image-1'],
    type: 'image',
    priceUSD: 0.038,
  },
  {
    merchantId: 5,
    title: 'Embedding 向量供应',
    summary: '高维语义向量，RAG 检索友好，成本低廉。',
    models: ['text-embedding-3-large'],
    type: 'embedding',
    priceUSD: 0.12,
  },
  {
    merchantId: 6,
    title: 'DeepSeek 特惠中转',
    summary: '个人节点，价格亲民，适合轻量试用。',
    models: ['deepseek-v3.2'],
    type: 'chat',
    priceUSD: 0.22,
  },
  {
    merchantId: 7,
    title: 'Qwen3 通义供应',
    summary: '通义千问旗舰，中英文与工具调用均衡。',
    models: ['qwen3-max', 'qwen3-vl-plus'],
    type: 'chat',
    priceUSD: 1.1,
  },
  {
    merchantId: 8,
    title: 'Kimi 长文中转',
    summary: '长文与 Agent 能力突出，价格随行就市。',
    models: ['kimi-k2.5'],
    type: 'chat',
    priceUSD: 0.58,
  },
  {
    merchantId: 9,
    title: 'Grok-4 实时供应',
    summary: '实时联网大模型，时效性问题回答见长。',
    models: ['grok-4', 'grok-4-fast'],
    type: 'chat',
    priceUSD: 2.8,
  },
  {
    merchantId: 10,
    title: 'Claude Sonnet 直连专线',
    summary: '官方直连低延迟专线，编码与 Agent 首选。',
    models: ['claude-sonnet-4.5'],
    type: 'chat',
    priceUSD: 2.85,
  },
  {
    merchantId: 10,
    title: 'GLM-4.6 国产合规供应',
    summary: '智谱旗舰，代码与推理均衡，国产合规首选。',
    models: ['glm-4.6'],
    type: 'chat',
    priceUSD: 0.55,
  },
  {
    merchantId: 11,
    title: 'Gemini Flash 廉价池',
    summary: '高速轻量多模态，适合规模化调用。',
    models: ['gemini-2.5-flash'],
    type: 'chat',
    priceUSD: 0.28,
  },
  {
    merchantId: 12,
    title: '多模型聚合网关',
    summary: '一个入口聚合主流模型，自动路由最优渠道。',
    models: ['gpt-4o', 'claude-sonnet-4.5', 'gemini-2.5-pro', 'deepseek-v3.2'],
    type: 'chat',
    priceUSD: 1.6,
  },
  {
    merchantId: 13,
    title: 'imagen-4 出图节点',
    summary: '写实级图像生成，细节与构图出色。',
    models: ['imagen-4'],
    type: 'image',
    priceUSD: 0.042,
  },
  {
    merchantId: 14,
    title: 'Claude Opus 旗舰供应',
    summary: '最强推理旗舰，复杂任务质量优先。',
    models: ['claude-opus-4.1'],
    type: 'chat',
    priceUSD: 14.5,
  },
  {
    merchantId: 15,
    title: 'DeepSeek-R1 推理中转',
    summary: '开源推理模型，思维链透明，性价比高。',
    models: ['deepseek-r1'],
    type: 'chat',
    priceUSD: 0.52,
  },
  {
    merchantId: 16,
    title: 'GPT 系列企业供应',
    summary: '鲲鹏企业级接入，多区域容灾，稳定首选。',
    models: ['gpt-4o', 'gpt-4o-mini', 'o3'],
    type: 'chat',
    priceUSD: 2.4,
  },
  {
    merchantId: 17,
    title: 'cogview-4 中文出图',
    summary: '中文语义友好的图像生成，支持中文提示词。',
    models: ['cogview-4'],
    type: 'image',
    priceUSD: 0.028,
  },
  {
    merchantId: 18,
    title: 'glm-4-voice 语音供应',
    summary: '端到端语音模型，支持情感语调与实时对话。',
    models: ['glm-4-voice'],
    type: 'audio',
    priceUSD: 0.019,
  },
  {
    merchantId: 3,
    title: 'Gemini Flash 高速中转',
    summary: '极光节点低延迟通道，长上下文与低成本兼顾。',
    models: ['gemini-2.5-flash'],
    type: 'chat',
    priceUSD: 0.3,
  },
  {
    merchantId: 9,
    title: 'Grok-4-fast 长窗供应',
    summary: '2M 超长窗口高速版，适合大文档场景。',
    models: ['grok-4-fast'],
    type: 'chat',
    priceUSD: 0.19,
  },
  {
    merchantId: 2,
    title: 'Qwen VL 视觉供应',
    summary: '视觉语言多模态，图文理解与文档解析强。',
    models: ['qwen3-vl-plus'],
    type: 'chat',
    priceUSD: 0.78,
  },
  /* ---- official platform supply ---- */
  {
    merchantId: PLATFORM_MERCHANT_ID,
    title: '平台官方 GPT 直供',
    summary: '平台自营 OpenAI 官方直连，SLA 与计费由平台兜底。',
    models: ['gpt-4o', 'gpt-4o-mini', 'o3'],
    type: 'chat',
    priceUSD: 2.5,
  },
  {
    merchantId: PLATFORM_MERCHANT_ID,
    title: '平台官方 Claude 直供',
    summary: '平台自营 Anthropic 官方通道，长上下文稳定无阉割。',
    models: ['claude-sonnet-4.5', 'claude-opus-4.1', 'claude-haiku-4.5'],
    type: 'chat',
    priceUSD: 3.0,
  },
  {
    merchantId: PLATFORM_MERCHANT_ID,
    title: '平台官方 Gemini 直供',
    summary: '平台自营 GCP Vertex 通道，多模态与超长窗口全量支持。',
    models: ['gemini-2.5-pro', 'gemini-2.5-flash'],
    type: 'chat',
    priceUSD: 1.25,
  },
  {
    merchantId: PLATFORM_MERCHANT_ID,
    title: '平台官方国产模型池',
    summary: 'DeepSeek / Qwen / GLM 官方渠道聚合，国产合规首选。',
    models: ['deepseek-v3.2', 'qwen3-max', 'glm-4.6'],
    type: 'chat',
    priceUSD: 0.6,
  },
]

/** Listings owned by the current mock user (shown on the sell side). */
const selfListingSeed: Array<{
  title: string
  summary: string
  models: string[]
  type: MarketModelType
  priceUSD: number
  status: ListingStatus
}> = [
  {
    title: '我的 GPT-4o 转售供应',
    summary: '自有 Azure 额度转售，稳定余量充足。',
    models: ['gpt-4o', 'gpt-4o-mini'],
    type: 'chat',
    priceUSD: 2.35,
    status: 'active',
  },
  {
    title: 'DeepSeek 闲置额度出让',
    summary: '团队闲置额度，低价出让，先到先得。',
    models: ['deepseek-v3.2'],
    type: 'chat',
    priceUSD: 0.24,
    status: 'active',
  },
  {
    title: 'Claude Sonnet 拼车供应',
    summary: '拼车共享官方额度，按量结算。',
    models: ['claude-sonnet-4.5'],
    type: 'chat',
    priceUSD: 2.9,
    status: 'reviewing',
  },
  {
    title: 'gpt-image 出图余量',
    summary: '出图额度余量清仓，按张计费。',
    models: ['gpt-image-1'],
    type: 'image',
    priceUSD: 0.036,
    status: 'delisted',
  },
]
/** Pick `count` distinct tags deterministically from the pool. */
function pickTags(count: number): string[] {
  const pool = [...marketTagPool]
  const out: string[] = []
  for (let i = 0; i < count && pool.length; i++) {
    out.push(pool.splice(Math.floor(rand() * pool.length), 1)[0])
  }
  return out
}

function qcSvg(label: string, tone: string): string {
  const svg =
    `<svg xmlns="http://www.w3.org/2000/svg" width="640" height="400" viewBox="0 0 640 400">` +
    `<rect width="640" height="400" fill="#1f232e"/>` +
    `<rect x="24" y="24" width="592" height="352" rx="12" fill="none" stroke="${tone}" stroke-width="2" stroke-dasharray="6 6"/>` +
    `<polyline points="60,300 160,220 260,260 360,150 460,190 560,90" fill="none" stroke="${tone}" stroke-width="4" stroke-linecap="round"/>` +
    `<text x="320" y="356" text-anchor="middle" font-family="monospace" font-size="22" fill="${tone}">${label}</text>` +
    `</svg>`
  return `data:image/svg+xml;utf8,${encodeURIComponent(svg)}`
}

const QC_TONES = ['#d97a45', '#7aa2d9', '#7ad98f']
/** Give roughly one in three listings 1-3 QC screenshots. */
function seedQcImages(listingId: number): string[] | undefined {
  if (listingId % 3 !== 0) return undefined
  const count = 1 + (listingId % 2) + (listingId % 5 === 0 ? 1 : 0) // 1..3
  return Array.from({ length: count }, (_, index) =>
    qcSvg(
      `QC 实测 #${listingId}-${index + 1} · 延迟采样`,
      QC_TONES[index % QC_TONES.length]
    )
  )
}

const merchantById = new Map(marketMerchants.map((m) => [m.id, m]))

let nextListingId = 1
const publicListings: MarketListing[] = listingSeed.map((seed) => {
  const merchant = merchantById.get(seed.merchantId)!
  // Larger scales (and the official platform) trend toward higher availability / QC.
  const scaleFloor =
    merchant.scale === 'platform'
      ? 98
      : merchant.scale === 'empire'
        ? 96
        : merchant.scale === 'studio'
          ? 90
          : merchant.scale === 'workshop'
            ? 85
            : 82
  const availability = Math.min(99.99, scaleFloor + rand() * (100 - scaleFloor))
  const qcScore = Math.round(
    Math.min(100, scaleFloor + rand() * (101 - scaleFloor))
  )
  const id = nextListingId++
  return {
    id,
    merchantId: seed.merchantId,
    title: seed.title,
    summary: seed.summary,
    source: marketSources[Math.floor(rand() * marketSources.length)],
    availability: Math.round(availability * 100) / 100,
    supportedModels: seed.models,
    qcScore,
    tags: pickTags(2 + Math.floor(rand() * 2)),
    priceUSD: seed.priceUSD,
    type: seed.type,
    listedAt: now - Math.floor(3 + rand() * 300) * DAY,
    rating: Math.round((3.6 + rand() * 1.4) * 10) / 10,
    reviewCount: Math.floor(rand() * 480) + 12,
    status: 'active',
    modelVendors: [
      ...new Set(seed.models.map((m) => modelVendorMap[m]).filter(Boolean)),
    ],
    qcImages: seedQcImages(id),
  }
})

const selfListings: MarketListing[] = selfListingSeed.map((seed) => {
  const salesCount = Math.floor(rand() * 80)
  // earningsUSD ≈ sales × price × 0.8 (platform fee deducted), rounded to cents.
  const earningsUSD = Math.round(salesCount * seed.priceUSD * 0.8 * 100) / 100
  return {
    id: nextListingId++,
    merchantId: 1, // display grouping only; ownerUid marks it as "mine"
    title: seed.title,
    summary: seed.summary,
    source: marketSources[Math.floor(rand() * marketSources.length)],
    availability: Math.round((94 + rand() * 5) * 100) / 100,
    supportedModels: seed.models,
    qcScore: Math.round(90 + rand() * 10),
    tags: pickTags(2),
    priceUSD: seed.priceUSD,
    type: seed.type,
    listedAt: now - Math.floor(3 + rand() * 120) * DAY,
    rating: Math.round((4 + rand() * 1) * 10) / 10,
    reviewCount: salesCount,
    status: seed.status,
    ownerUid: mockUser.id,
    earningsUSD,
    modelVendors: [
      ...new Set(seed.models.map((m) => modelVendorMap[m]).filter(Boolean)),
    ],
  }
})

export const marketListings: MarketListing[] = [
  ...publicListings,
  ...selfListings,
]

// Distinct upstream sources, used for the "available channels" toolbar stat.
export const marketplaceChannels: string[] = [
  ...new Set(marketListings.map((l) => l.source)),
]

// Backfill each merchant's channel count from its active listings' sources.
for (const m of marketMerchants) {
  const sources = new Set(
    publicListings.filter((l) => l.merchantId === m.id).map((l) => l.source)
  )
  m.channelCount = sources.size
}

let nextMyChannelId = 1
/** Seeded from a few public listings so Models-page channels have data. */
export const myChannels: MyChannel[] = [1, 3, 6, 14].map((listingId, i) => {
  const listing = publicListings.find((l) => l.id === listingId)!
  return {
    id: nextMyChannelId++,
    listingId: listing.id,
    merchantId: listing.merchantId,
    merchantName: merchantById.get(listing.merchantId)!.name,
    title: listing.title,
    supportedModels: listing.supportedModels,
    status: i === 3 ? 'disabled' : 'active',
    addedAt: now - Math.floor(5 + rand() * 60) * DAY,
  }
})

/** Append a listing to myChannels (id bookkeeping lives here). */
export function addMyChannel(listing: MarketListing): MyChannel {
  const item: MyChannel = {
    id: nextMyChannelId++,
    listingId: listing.id,
    merchantId: listing.merchantId,
    merchantName: merchantById.get(listing.merchantId)?.name ?? '未知商家',
    title: listing.title,
    supportedModels: listing.supportedModels,
    status: 'active',
    addedAt: Math.floor(Date.now() / 1000),
  }
  myChannels.unshift(item)
  return item
}

/** Channel display name for the official platform supply. */
export const PLATFORM_CHANNEL_NAME = '平台'

let nextInvoiceId = 10

export function resetMockDataCounters(): void {
  nextMyChannelId = 5
  nextInvoiceId = 10
}

export const invoices: InvoiceItem[] = [
  {
    id: 1,
    title: 'Acme 科技有限公司',
    tax_id: '91310000MA1FL5EM1G',
    amount: 1500,
    email: 'finance@acme.example.com',
    note: '',
    status: 'issued',
    pdf_url: 'https://example.com/invoice/fapiao_2026_001.pdf',
    reject_reason: '',
    created: now - 30 * DAY,
    updated: now - 28 * DAY,
  },
  {
    id: 2,
    title: '张三',
    tax_id: '',
    amount: 500,
    email: '',
    note: '个人增值税普通发票',
    status: 'pending',
    pdf_url: '',
    reject_reason: '',
    created: now - 3 * DAY,
    updated: now - 3 * DAY,
  },
  {
    id: 3,
    title: '旧版测试企业',
    tax_id: '91110000000000001A',
    amount: 200,
    email: 'test@example.com',
    note: '',
    status: 'rejected',
    pdf_url: '',
    reject_reason: '税号与公司名称不匹配，请核实后重新提交。',
    created: now - 14 * DAY,
    updated: now - 13 * DAY,
  },
]

/** Add an invoice application. */
export function addInvoice(
  data: Omit<
    InvoiceItem,
    'id' | 'status' | 'pdf_url' | 'reject_reason' | 'created' | 'updated'
  >
): InvoiceItem {
  const item: InvoiceItem = {
    ...data,
    id: nextInvoiceId++,
    status: 'pending',
    pdf_url: '',
    reject_reason: '',
    created: Math.floor(Date.now() / 1000),
    updated: Math.floor(Date.now() / 1000),
  }
  invoices.unshift(item)
  return item
}
