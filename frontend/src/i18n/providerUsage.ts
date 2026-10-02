export const providerUsageMessages = {
  'zh-CN': {
    title: 'Codex 供应商用量',
    refresh: '刷新供应商用量',
    autoRefresh: '每 5 分钟自动采集',
    autoEnabled: '自动采集已开启（每 5 分钟）',
    autoDisabled: '自动采集未开启',
    observationOnly:
      '供应商侧的用量快照，仅供观察，不代表网关余额或本次请求是否可用。',
    empty: '尚未采集用量。可手动刷新，或开启自动采集。',
    requestFailed: '用量操作失败，请稍后重试。',
    states: {
      known: '已更新',
      unknown: '未知',
      stale: '已过期',
      error: '刷新失败',
    },
    plan: '套餐',
    fetchedAt: '采集时间',
    lastAttempt: '最近刷新',
    retryAt: '下次可刷新',
    unknown: '未知',
    used: '已用 {value}%',
    resetAt: '重置时间',
    limit: '窗口长度：{seconds} 秒',
    windows: {
      rate_limit_primary: '主要用量窗口',
      rate_limit_secondary: '次要用量窗口',
      code_review_primary: '代码审查主要窗口',
      code_review_secondary: '代码审查次要窗口',
    },
    errors: {
      upstream_unavailable: '供应商暂时不可用或响应无效。',
      upstream_rejected: '供应商拒绝读取用量。',
      rate_limited: '供应商限制了刷新频率，等待后再试。',
      credential_refresh_failed: '凭据刷新未完成，请检查渠道凭据。',
    },
  },
  en: {
    title: 'Codex provider usage',
    refresh: 'Refresh provider usage',
    autoRefresh: 'Collect automatically every 5 minutes',
    autoEnabled: 'Automatic collection is on (every 5 minutes)',
    autoDisabled: 'Automatic collection is off',
    observationOnly:
      'A provider usage snapshot for observation. It does not represent gateway balance or request availability.',
    empty:
      'No usage has been collected. Refresh manually or enable automatic collection.',
    requestFailed: 'The usage operation failed. Please try again later.',
    states: {
      known: 'Current',
      unknown: 'Unknown',
      stale: 'Out of date',
      error: 'Refresh failed',
    },
    plan: 'Plan',
    fetchedAt: 'Collected at',
    lastAttempt: 'Last refresh',
    retryAt: 'Next refresh available',
    unknown: 'Unknown',
    used: '{value}% used',
    resetAt: 'Resets at',
    limit: 'Window length: {seconds} seconds',
    windows: {
      rate_limit_primary: 'Primary usage window',
      rate_limit_secondary: 'Secondary usage window',
      code_review_primary: 'Primary code review window',
      code_review_secondary: 'Secondary code review window',
    },
    errors: {
      upstream_unavailable:
        'The provider is unavailable or returned an invalid response.',
      upstream_rejected: 'The provider rejected the usage request.',
      rate_limited:
        'The provider limited refresh requests. Wait before trying again.',
      credential_refresh_failed:
        'Credential refresh did not complete. Check the channel credentials.',
    },
  },
}
