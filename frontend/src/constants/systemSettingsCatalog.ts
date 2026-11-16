import type { SystemSettingValue } from '@/composables/useSystemSettings'

export type SystemSettingsDomainId =
  'site' | 'auth' | 'billing' | 'models' | 'security' | 'content' | 'operations'

export type SystemSettingFieldKind =
  | 'boolean'
  | 'number'
  | 'text'
  | 'url'
  | 'secret'
  | 'secret-textarea'
  | 'textarea'
  | 'json'
  | 'list'
  | 'key-value'
  | 'ratio'
  | 'select'
  | 'role'
  | 'amount-list'

export interface SystemSettingOption {
  value: string
  labelKey: string
}

/** Display text is held as message keys and translated where it renders. */
export interface SystemSettingField {
  key: string
  labelKey: string
  descriptionKey?: string
  kind: SystemSettingFieldKind
  defaultValue: SystemSettingValue
  min?: number
  max?: number
  integer?: boolean
  options?: readonly SystemSettingOption[]
}

export interface SystemSettingsSection {
  id: string
  titleKey: string
  descriptionKey: string
  fields: readonly SystemSettingField[]
  /** Keys managed by a dedicated integration panel instead of generic fields. */
  managedKeys?: readonly string[]
  integration?:
    | 'custom-oauth'
    | 'performance'
    | 'channel-affinity'
    | 'waffo-pancake'
    | 'gmpay'
    | 'topup-bonus'
    | 'payment-compliance'
    | 'operations-maintenance'
}

export interface SystemSettingsDomain {
  id: SystemSettingsDomainId
  titleKey: string
  defaultSection: string
  sections: readonly SystemSettingsSection[]
}

// Field helpers take message ids under systemSettings.fields; select options
// take ids under systemSettings.options.
const fieldKey = (id: string) => `systemSettings.fields.${id}`

function field(
  kind: SystemSettingFieldKind,
  key: string,
  label: string,
  defaultValue: SystemSettingValue,
  description?: string
): SystemSettingField {
  return {
    key,
    labelKey: fieldKey(label),
    descriptionKey: description ? fieldKey(description) : undefined,
    kind,
    defaultValue,
  }
}

const text = (
  key: string,
  label: string,
  defaultValue = '',
  description?: string
) => field('text', key, label, defaultValue, description)
const url = (key: string, label: string, description?: string) =>
  field('url', key, label, '', description)
const secret = (key: string, label: string, description?: string) =>
  field('secret', key, label, '', description)
const secretTextarea = (key: string, label: string, description?: string) =>
  field('secret-textarea', key, label, '', description)
const number = (
  key: string,
  label: string,
  defaultValue = 0,
  description?: string
) => field('number', key, label, defaultValue, description)
const toggle = (
  key: string,
  label: string,
  defaultValue = false,
  description?: string
) => field('boolean', key, label, defaultValue, description)
const textarea = (key: string, label: string, description?: string) =>
  field('textarea', key, label, '', description)
const json = (
  key: string,
  label: string,
  defaultValue = '{}',
  description?: string
) => field('json', key, label, defaultValue, description)
const list = (
  key: string,
  label: string,
  defaultValue = '[]',
  description?: string
) => field('list', key, label, defaultValue, description)
const keyValue = (
  key: string,
  label: string,
  defaultValue = '{}',
  description?: string
) => field('key-value', key, label, defaultValue, description)
const option = (value: string, label: string): SystemSettingOption => ({
  value,
  labelKey: `systemSettings.options.${label}`,
})
const select = (
  key: string,
  label: string,
  defaultValue: string,
  options: readonly SystemSettingOption[],
  description?: string
): SystemSettingField => ({
  ...field('select', key, label, defaultValue, description),
  options,
})
const amountList = (key: string, label: string, description?: string) =>
  field('amount-list', key, label, '[]', description)

export const SYSTEM_SETTINGS_DOMAINS: readonly SystemSettingsDomain[] = [
  {
    id: 'site',
    titleKey: 'systemSettings.tabs.site',
    defaultSection: 'system-info',
    sections: [
      {
        id: 'system-info',
        titleKey: 'systemSettings.sections.systemInfo',
        descriptionKey: 'systemSettings.sections.systemInfoDescription',
        fields: [
          text('SystemName', 'systemName'),
          url('ServerAddress', 'serverAddress', 'serverAddressHint'),
          url('Logo', 'logoUrl'),
          url('general_setting.docs_link', 'docsLink'),
          textarea('Footer', 'footer'),
          textarea('About', 'about'),
          textarea('HomePageContent', 'homePageContent'),
          textarea('legal.user_agreement', 'userAgreement'),
          textarea('legal.privacy_policy', 'privacyPolicy'),
        ],
      },
      {
        id: 'notice',
        titleKey: 'systemSettings.sections.notice',
        descriptionKey: 'systemSettings.sections.noticeDescription',
        fields: [textarea('Notice', 'notice')],
      },
      {
        id: 'navigation',
        titleKey: 'systemSettings.sections.navigation',
        descriptionKey: 'systemSettings.sections.navigationDescription',
        fields: [
          json('HeaderNavModules', 'headerNavModules'),
          json('SidebarModulesAdmin', 'sidebarModulesAdmin'),
        ],
      },
    ],
  },
  {
    id: 'auth',
    titleKey: 'systemSettings.tabs.auth',
    defaultSection: 'basic-auth',
    sections: [
      {
        id: 'basic-auth',
        titleKey: 'systemSettings.sections.basicAuth',
        descriptionKey: 'systemSettings.sections.basicAuthDescription',
        fields: [
          toggle('PasswordLoginEnabled', 'passwordLogin', true),
          toggle('RegisterEnabled', 'registerEnabled', true),
          toggle('PasswordRegisterEnabled', 'passwordRegister', true),
          toggle('EmailVerificationEnabled', 'emailVerification'),
          toggle('EmailDomainRestrictionEnabled', 'emailDomainRestriction'),
          toggle('EmailAliasRestrictionEnabled', 'emailAliasRestriction'),
          textarea(
            'EmailDomainWhitelist',
            'emailDomainWhitelist',
            'emailDomainWhitelistHint'
          ),
        ],
      },
      {
        id: 'oauth',
        titleKey: 'systemSettings.sections.oauth',
        descriptionKey: 'systemSettings.sections.oauthDescription',
        fields: [
          toggle('GitHubOAuthEnabled', 'githubEnabled'),
          text('GitHubClientId', 'githubClientId'),
          secret('GitHubClientSecret', 'githubClientSecret'),
          toggle('LinuxDOOAuthEnabled', 'linuxdoEnabled'),
          text('LinuxDOClientId', 'linuxdoClientId'),
          secret('LinuxDOClientSecret', 'linuxdoClientSecret'),
          number('LinuxDOMinimumTrustLevel', 'linuxdoMinimumTrustLevel', 0),
          toggle('discord.enabled', 'discordEnabled'),
          text('discord.client_id', 'discordClientId'),
          secret('discord.client_secret', 'discordClientSecret'),
          text('telegram.client_id', 'telegramClientId'),
          secret('telegram.client_secret', 'telegramClientSecret'),
          toggle('TelegramOAuthEnabled', 'telegramEnabled'),
          secret('TelegramBotToken', 'telegramBotToken'),
          text('TelegramBotName', 'telegramBotName'),
          toggle('WeChatAuthEnabled', 'wechatEnabled'),
          url('WeChatServerAddress', 'wechatServerAddress'),
          secret('WeChatServerToken', 'wechatServerToken'),
          url('WeChatAccountQRCodeImageURL', 'wechatQrCodeUrl'),
          toggle('oidc.enabled', 'oidcEnabled'),
          text('oidc.display_name', 'oidcDisplayName'),
          text('oidc.client_id', 'oidcClientId'),
          secret('oidc.client_secret', 'oidcClientSecret'),
          url('oidc.well_known', 'oidcWellKnown'),
          url('oidc.authorization_endpoint', 'oidcAuthorizationEndpoint'),
          url('oidc.token_endpoint', 'oidcTokenEndpoint'),
          url('oidc.user_info_endpoint', 'oidcUserInfoEndpoint'),
        ],
      },
      {
        id: 'passkey',
        managedKeys: [
          'passkey.rp_id',
          'passkey.legacy_rp_ids',
          'passkey.origins',
        ],
        titleKey: 'systemSettings.sections.passkey',
        descriptionKey: 'systemSettings.sections.passkeyDescription',
        fields: [
          toggle('passkey.enabled', 'passkeyEnabled'),
          text('passkey.rp_display_name', 'passkeyRpDisplayName'),
          toggle('passkey.allow_insecure_origin', 'passkeyAllowInsecureOrigin'),
          select(
            'passkey.user_verification',
            'passkeyUserVerification',
            'preferred',
            [
              option('required', 'required'),
              option('preferred', 'preferred'),
              option('discouraged', 'discouraged'),
            ]
          ),
          select('passkey.attachment_preference', 'passkeyAttachment', '', [
            option('', 'anyAuthenticator'),
            option('platform', 'platformAuthenticator'),
            option('cross-platform', 'crossPlatformAuthenticator'),
          ]),
        ],
      },
      {
        id: 'bot-protection',
        titleKey: 'systemSettings.sections.botProtection',
        descriptionKey: 'systemSettings.sections.botProtectionDescription',
        fields: [
          toggle('TurnstileCheckEnabled', 'turnstileEnabled'),
          text(
            'TurnstileSiteKey',
            'turnstileSiteKey',
            '',
            'turnstileSiteKeyHint'
          ),
          secret('TurnstileSecretKey', 'turnstileSecretKey'),
        ],
      },
      {
        id: 'custom-oauth',
        titleKey: 'systemSettings.sections.customOAuth',
        descriptionKey: 'systemSettings.sections.customOAuthDescription',
        fields: [],
        integration: 'custom-oauth',
      },
    ],
  },
  {
    id: 'billing',
    titleKey: 'systemSettings.tabs.billing',
    defaultSection: 'quota',
    sections: [
      {
        id: 'topup-bonus',
        titleKey: 'systemSettings.sections.topupBonus',
        descriptionKey: 'systemSettings.sections.topupBonusDescription',
        fields: [],
        integration: 'topup-bonus',
      },
      {
        id: 'affiliate',
        titleKey: 'systemSettings.sections.affiliate',
        descriptionKey: 'systemSettings.sections.affiliateDescription',
        fields: [
          toggle('affiliate_setting.enabled', 'affiliateEnabled'),
          {
            ...number('affiliate_setting.rate_bps', 'affiliateRateBps', 2000),
            min: 0,
            max: 10000,
            integer: true,
          },
          {
            ...number(
              'affiliate_setting.freeze_hours',
              'affiliateFreezeHours',
              0
            ),
            min: 0,
            max: 720,
            integer: true,
          },
          {
            ...number(
              'affiliate_setting.duration_days',
              'affiliateDurationDays',
              0
            ),
            min: 0,
            max: 3650,
            integer: true,
          },
          {
            ...number(
              'affiliate_setting.per_invitee_cap_quota',
              'affiliateCapQuota',
              0
            ),
            min: 0,
            integer: true,
          },
          toggle(
            'affiliate_setting.admin_recharge_enabled',
            'affiliateAdminRecharge'
          ),
        ],
      },
      {
        id: 'quota',
        titleKey: 'systemSettings.sections.quota',
        descriptionKey: 'systemSettings.sections.quotaDescription',
        fields: [
          text('QuotaForNewUser', 'newUserQuota', '0'),
          {
            ...number(
              'quota_setting.pre_consume_multiplier',
              'preConsumeMultiplier',
              1
            ),
            descriptionKey: fieldKey('preConsumeMultiplierHint'),
          },
          url('TopUpLink', 'topUpLink'),
          number('USDExchangeRate', 'usdExchangeRate', 1),
          toggle('DisplayInCurrencyEnabled', 'displayInCurrency', true),
          toggle('DisplayTokenStatEnabled', 'displayTokenStat'),
          select(
            'general_setting.quota_display_type',
            'quotaDisplayType',
            'USD',
            [
              option('CNY', 'cny'),
              option('USD', 'usd'),
              option('TOKENS', 'tokens'),
              option('CUSTOM', 'customCurrency'),
            ]
          ),
          text(
            'general_setting.custom_currency_symbol',
            'customCurrencySymbol',
            '¤'
          ),
          number(
            'general_setting.custom_currency_exchange_rate',
            'customCurrencyRate',
            1
          ),
        ],
      },
      {
        id: 'payment',
        titleKey: 'systemSettings.sections.payment',
        descriptionKey: 'systemSettings.sections.paymentDescription',
        fields: [
          url('CustomCallbackAddress', 'callbackAddress', 'callbackHint'),
          secret('StripeApiSecret', 'stripeApiSecret'),
          secret('StripeWebhookSecret', 'stripeWebhookSecret'),
          text('StripePriceId', 'stripePriceId'),
          number('StripeUnitPrice', 'stripeUnitPrice', 1),
          number('StripeMinTopUp', 'stripeMinTopUp', 1),
          toggle('StripePromotionCodesEnabled', 'stripePromotionCodes'),
          secret('CreemApiKey', 'creemApiKey'),
          secret('CreemWebhookSecret', 'creemWebhookSecret'),
          toggle('CreemTestMode', 'creemTestMode'),
          json('CreemProducts', 'creemProducts', '[]'),
          toggle('WaffoEnabled', 'waffoEnabled'),
          secret('WaffoApiKey', 'waffoApiKey'),
          secret('WaffoPrivateKey', 'waffoPrivateKey'),
          secretTextarea('WaffoPublicCert', 'waffoPublicCert'),
          toggle('WaffoSandbox', 'waffoSandbox'),
          text('WaffoMerchantId', 'waffoMerchantId'),
          text('WaffoCurrency', 'waffoCurrency', 'USD'),
          number('WaffoUnitPrice', 'waffoUnitPrice', 1),
          number('WaffoMinTopUp', 'waffoMinTopUp', 1),
          url('WaffoNotifyUrl', 'waffoNotifyUrl'),
          url('WaffoReturnUrl', 'waffoReturnUrl'),
          json('WaffoPayMethods', 'waffoPayMethods', '[]'),
          amountList(
            'payment_setting.amount_options',
            'amountOptions',
            'amountOptionsHint'
          ),
          secret('WaffoSandboxApiKey', 'waffoSandboxApiKey'),
          secret('WaffoSandboxPrivateKey', 'waffoSandboxPrivateKey'),
          secretTextarea('WaffoSandboxPublicCert', 'waffoSandboxPublicCert'),
        ],
        integration: 'payment-compliance',
      },
      {
        id: 'epay',
        titleKey: 'systemSettings.sections.epay',
        descriptionKey: 'systemSettings.sections.epayDescription',
        fields: [
          url('PayAddress', 'epayAddress'),
          text('EpayId', 'merchantId'),
          secret('EpayKey', 'merchantSecret', 'secretHint'),
          {
            ...secretTextarea(
              'payment_setting.epay_merchant_private_key',
              'merchantSecret'
            ),
            labelKey: 'pluginExtensions.rsaPrivate',
          },
          {
            ...secretTextarea(
              'payment_setting.epay_platform_public_key',
              'merchantSecret'
            ),
            labelKey: 'pluginExtensions.rsaPublic',
          },
          {
            key: 'payment_setting.epay_sign_type',
            labelKey: 'pluginExtensions.epayVersion',
            kind: 'select',
            defaultValue: 'MD5',
            options: [
              { value: 'MD5', labelKey: 'pluginExtensions.md5' },
              { value: 'RSA', labelKey: 'pluginExtensions.rsa' },
            ],
          },
          { ...number('Price', 'epayPrice', 7.3), min: 0 },
          {
            ...number('MinTopUp', 'minimumUsd', 1),
            min: 1,
            max: 18446744073709,
            integer: true,
          },
          json('PayMethods', 'epayMethods', '[]', 'epayMethodsHint'),
        ],
      },
      {
        id: 'gmpay',
        titleKey: 'systemSettings.gmpayPanel.title',
        descriptionKey: 'systemSettings.gmpayPanel.description',
        fields: [],
        integration: 'gmpay',
      },
      {
        id: 'waffo-pancake',
        titleKey: 'systemSettings.sections.waffoPancake',
        descriptionKey: 'systemSettings.sections.waffoPancakeDescription',
        fields: [],
        managedKeys: [
          'WaffoPancakeMerchantID',
          'WaffoPancakePrivateKey',
          'WaffoPancakeReturnURL',
          'WaffoPancakeStoreID',
          'WaffoPancakeProductID',
          'WaffoPancakeUnitPrice',
          'WaffoPancakeMinTopUp',
        ],
        integration: 'waffo-pancake',
      },
      {
        id: 'checkin',
        titleKey: 'systemSettings.sections.checkin',
        descriptionKey: 'systemSettings.sections.checkinDescription',
        fields: [
          toggle('checkin_setting.enabled', 'checkinEnabled'),
          text('checkin_setting.min_quota', 'checkinMinQuota', '100'),
          text('checkin_setting.max_quota', 'checkinMaxQuota', '500'),
        ],
      },
    ],
  },
  {
    id: 'models',
    titleKey: 'systemSettings.tabs.models',
    defaultSection: 'routing',
    sections: [
      {
        id: 'routing',
        titleKey: 'systemSettings.sections.routing',
        descriptionKey: 'systemSettings.sections.routingDescription',
        fields: [
          { ...number('RetryTimes', 'retryTimes'), min: 0, integer: true },
          number('ChannelDisableThreshold', 'channelDisableThreshold', 5),
          toggle('AutomaticDisableChannelEnabled', 'autoDisableChannel'),
          toggle('AutomaticEnableChannelEnabled', 'autoEnableChannel'),
          textarea('AutomaticDisableKeywords', 'autoDisableKeywords'),
          textarea('AutomaticDisableStatusCodes', 'autoDisableStatusCodes'),
          textarea('AutomaticRetryStatusCodes', 'autoRetryStatusCodes'),
          toggle(
            'monitor_setting.auto_test_channel_enabled',
            'autoTestChannel'
          ),
          number(
            'monitor_setting.channel_test_concurrency',
            'channelTestConcurrency',
            1
          ),
          number(
            'monitor_setting.auto_test_channel_minutes',
            'autoTestMinutes',
            60
          ),
          toggle('global.pass_through_request_enabled', 'passThroughRequest'),
          list(
            'global.thinking_model_blacklist',
            'thinkingModelBlacklist',
            '[]',
            'thinkingModelBlacklistHint'
          ),
          json(
            'global.chat_completions_to_responses_policy',
            'chatCompletionsToResponses',
            '{}'
          ),
          toggle('general_setting.ping_interval_enabled', 'pingInterval'),
          number(
            'general_setting.ping_interval_seconds',
            'pingIntervalSeconds',
            60
          ),
          select(
            'monitor_setting.channel_test_mode',
            'channelTestMode',
            'scheduled_all',
            [
              option('scheduled_all', 'scheduledAll'),
              option('auto_ban_only', 'autoBanOnly'),
              option('passive_recovery', 'passiveRecovery'),
            ]
          ),
        ],
      },
      {
        id: 'affinity',
        titleKey: 'systemSettings.sections.affinity',
        descriptionKey: 'systemSettings.sections.affinityDescription',
        fields: [
          toggle('channel_affinity_setting.enabled', 'affinityEnabled'),
          toggle(
            'channel_affinity_setting.switch_on_success',
            'affinitySwitchOnSuccess'
          ),
          number(
            'channel_affinity_setting.default_ttl_seconds',
            'affinityTtl',
            3600
          ),
          toggle(
            'channel_affinity_setting.keep_on_channel_disabled',
            'affinityKeepOnDisabled'
          ),
          number(
            'channel_affinity_setting.max_entries',
            'affinityMaxEntries',
            100000
          ),
          json('channel_affinity_setting.rules', 'affinityRules', '[]'),
        ],
        integration: 'channel-affinity',
      },
      {
        id: 'vendor',
        titleKey: 'systemSettings.sections.vendor',
        descriptionKey: 'systemSettings.sections.vendorDescription',
        fields: [
          keyValue('gemini.safety_settings', 'geminiSafety'),
          json('claude.default_max_tokens', 'claudeDefaultMaxTokens'),
          toggle(
            'claude.thinking_adapter_enabled',
            'claudeThinkingAdapter',
            false,
            'claudeThinkingAdapterHint'
          ),
          number(
            'claude.thinking_adapter_budget_tokens_percentage',
            'claudeThinkingBudget',
            0.8
          ),
          json('claude.model_headers_settings', 'claudeModelHeaders', '{}'),
          json('gemini.version_settings', 'geminiVersions', '{}'),
          list('gemini.supported_imagine_models', 'geminiImagineModels', '[]'),
          toggle(
            'gemini.thinking_adapter_enabled',
            'geminiThinkingAdapter',
            false,
            'geminiThinkingAdapterHint'
          ),
          number(
            'gemini.thinking_adapter_budget_tokens_percentage',
            'geminiThinkingBudget',
            0.6
          ),
          toggle(
            'gemini.function_call_thought_signature_enabled',
            'geminiThoughtSignature',
            true,
            'geminiThoughtSignatureHint'
          ),
          toggle(
            'gemini.remove_function_response_id_enabled',
            'geminiRemoveFunctionResponseId'
          ),
          toggle('grok.violation_deduction_enabled', 'grokViolationDeduction'),
          {
            ...number(
              'grok.violation_deduction_amount',
              'grokViolationDeductionAmount',
              0.05
            ),
            min: 0,
          },
        ],
      },
    ],
  },
  {
    id: 'security',
    titleKey: 'systemSettings.tabs.security',
    defaultSection: 'rate-limit',
    sections: [
      {
        id: 'rate-limit',
        titleKey: 'systemSettings.sections.rateLimit',
        descriptionKey: 'systemSettings.sections.rateLimitDescription',
        fields: [
          toggle('ModelRequestRateLimitEnabled', 'rateLimitEnabled'),
          {
            ...number('ModelRequestRateLimitCount', 'rateLimitCount', 60),
            min: 0,
            max: 100_000_000,
            integer: true,
          },
          {
            ...number(
              'ModelRequestRateLimitSuccessCount',
              'rateLimitSuccessCount',
              1000
            ),
            min: 1,
            max: 100_000_000,
            integer: true,
          },
          {
            ...number(
              'ModelRequestRateLimitDurationMinutes',
              'rateLimitDuration',
              1
            ),
            min: 0,
            integer: true,
          },
        ],
      },
      {
        id: 'ssrf',
        titleKey: 'systemSettings.sections.ssrf',
        descriptionKey: 'systemSettings.sections.ssrfDescription',
        fields: [
          toggle('fetch_setting.enable_ssrf_protection', 'ssrfEnabled', true),
          toggle('fetch_setting.allow_private_ip', 'ssrfAllowPrivateIp'),
          toggle('fetch_setting.domain_filter_mode', 'ssrfDomainAllowlist'),
          toggle('fetch_setting.ip_filter_mode', 'ssrfIpAllowlist'),
          list('fetch_setting.domain_list', 'ssrfDomains', '[]'),
          list('fetch_setting.ip_list', 'ssrfIps', '[]'),
          list(
            'fetch_setting.allowed_ports',
            'ssrfPorts',
            '["80", "443", "8080", "8443"]'
          ),
          toggle(
            'fetch_setting.apply_ip_filter_for_domain',
            'ssrfApplyIpFilterToDomains',
            true
          ),
        ],
      },
      {
        id: 'token-limits',
        titleKey: 'systemSettings.sections.tokenLimits',
        descriptionKey: 'systemSettings.sections.tokenLimitsDescription',
        fields: [number('token_setting.max_user_tokens', 'maxUserTokens')],
      },
    ],
  },
  {
    id: 'content',
    titleKey: 'systemSettings.tabs.content',
    defaultSection: 'dashboard',
    sections: [
      {
        id: 'dashboard',
        titleKey: 'systemSettings.sections.dashboard',
        descriptionKey: 'systemSettings.sections.dashboardDescription',
        fields: [
          toggle('DataExportEnabled', 'dataExportEnabled', true),
          {
            ...number('DataExportInterval', 'dataExportInterval', 5),
            min: 1,
            max: 1440,
            integer: true,
          },
          select('DataExportDefaultTime', 'dataExportDefaultTime', 'hour', [
            option('hour', 'hour'),
            option('day', 'day'),
            option('week', 'week'),
          ]),
        ],
      },
      {
        id: 'console-content',
        titleKey: 'systemSettings.sections.consoleContent',
        descriptionKey: 'systemSettings.sections.consoleContentDescription',
        fields: [
          toggle(
            'console_setting.announcements_enabled',
            'announcementsEnabled'
          ),
          json('console_setting.announcements', 'announcements', '[]'),
          toggle('console_setting.api_info_enabled', 'apiInfoEnabled'),
          json('console_setting.api_info', 'apiInfo', '[]'),
          toggle('console_setting.faq_enabled', 'faqEnabled'),
          json('console_setting.faq', 'faq', '[]'),
          toggle('console_setting.uptime_kuma_enabled', 'uptimeKumaEnabled'),
          json('console_setting.uptime_kuma_groups', 'uptimeKumaGroups', '[]'),
        ],
      },
      {
        id: 'drawing',
        titleKey: 'systemSettings.sections.drawing',
        descriptionKey: 'systemSettings.sections.drawingDescription',
        fields: [
          toggle('DrawingEnabled', 'drawingEnabled'),
          toggle('MjNotifyEnabled', 'mjNotify'),
          toggle('MjAccountFilterEnabled', 'mjAccountFilter'),
          toggle('MjForwardUrlEnabled', 'mjForwardUrl'),
          toggle('MjModeClearEnabled', 'mjModeClear'),
          toggle('MjActionCheckSuccessEnabled', 'mjActionCheck'),
        ],
      },
    ],
  },
  {
    id: 'operations',
    titleKey: 'systemSettings.tabs.operations',
    defaultSection: 'behavior',
    sections: [
      {
        id: 'behavior',
        titleKey: 'systemSettings.sections.behavior',
        descriptionKey: 'systemSettings.sections.behaviorDescription',
        fields: [
          toggle('DefaultCollapseSidebar', 'defaultCollapseSidebar'),
          toggle('DemoSiteEnabled', 'demoSite'),
          toggle('SelfUseModeEnabled', 'selfUseMode'),
          toggle('LogConsumeEnabled', 'logConsume', true),
          toggle('log_setting.user_export_enabled', 'logUserExport', true),
          {
            ...number(
              'log_setting.max_range_days',
              'logMaxRangeDays',
              365,
              'logMaxRangeDaysHint'
            ),
            min: 1,
            integer: true,
          },
        ],
      },
      {
        id: 'system-tasks',
        titleKey: 'pluginExtensions.retention',
        descriptionKey: 'pluginExtensions.retention',
        fields: [
          {
            key: 'system_task_setting.retention_days',
            kind: 'number',
            labelKey: 'pluginExtensions.retention',
            defaultValue: 30,
            min: 1,
            integer: true,
          },
        ],
      },
      {
        id: 'email',
        titleKey: 'systemSettings.sections.email',
        descriptionKey: 'systemSettings.sections.emailDescription',
        fields: [
          text('SMTPServer', 'smtpServer'),
          {
            ...number('SMTPPort', 'smtpPort', 587),
            min: 1,
            max: 65535,
            integer: true,
          },
          text('SMTPAccount', 'smtpAccount'),
          text('SMTPFrom', 'smtpFrom'),
          secret('SMTPToken', 'smtpToken'),
          toggle('SMTPSSLEnabled', 'smtpSsl'),
          toggle('SMTPStartTLSEnabled', 'smtpStartTls'),
          toggle('SMTPInsecureSkipVerify', 'smtpSkipVerify'),
          toggle('SMTPForceAuthLogin', 'smtpForceAuthLogin'),
          text('ResendFrom', 'resendFrom', '', 'resendFromHint'),
          secret('ResendApiKey', 'resendApiKey'),
          // Declared after the Resend fields so a save switches the method
          // only after its credentials; the backend validates the stored
          // Resend settings when the method becomes resend.
          select(
            'EmailProvider',
            'emailProvider',
            'smtp',
            [option('smtp', 'emailSmtp'), option('resend', 'emailResend')],
            'emailProviderHint'
          ),
        ],
      },
      {
        id: 'monitoring',
        titleKey: 'systemSettings.sections.monitoring',
        descriptionKey: 'systemSettings.sections.monitoringDescription',
        fields: [
          toggle(
            'QuotaRemindEnabled',
            'quotaRemindEnabled',
            true,
            'quotaRemindEnabledHint'
          ),
          text('QuotaRemindThreshold', 'quotaRemindThreshold', '1000'),
          toggle('perf_metrics_setting.enabled', 'perfMetricsEnabled', true),
          {
            ...number(
              'perf_metrics_setting.flush_interval',
              'perfMetricsFlushInterval',
              5
            ),
            min: 1,
            integer: true,
          },
          select(
            'perf_metrics_setting.bucket_time',
            'perfMetricsBucket',
            'hour',
            [
              option('minute', 'minute'),
              option('5min', 'fiveMinutes'),
              option('hour', 'hour'),
            ]
          ),
          {
            ...number(
              'perf_metrics_setting.retention_days',
              'perfMetricsRetention'
            ),
            min: 0,
            integer: true,
          },
        ],
      },
      {
        id: 'worker',
        titleKey: 'systemSettings.sections.worker',
        descriptionKey: 'systemSettings.sections.workerDescription',
        fields: [
          url('WorkerUrl', 'workerUrl'),
          secret('WorkerValidKey', 'workerValidKey'),
          toggle('WorkerAllowHttpImageRequestEnabled', 'workerAllowHttpImage'),
        ],
      },
      {
        id: 'performance',
        titleKey: 'systemSettings.sections.performance',
        descriptionKey: 'systemSettings.sections.performanceDescription',
        fields: [
          toggle('performance_setting.disk_cache_enabled', 'diskCacheEnabled'),
          {
            ...number(
              'performance_setting.disk_cache_threshold_mb',
              'diskCacheThreshold',
              10
            ),
            min: 1,
            integer: true,
          },
          {
            ...number(
              'performance_setting.disk_cache_max_size_mb',
              'diskCacheMaxSize',
              1024
            ),
            min: 100,
            integer: true,
          },
          text('performance_setting.disk_cache_path', 'diskCachePath'),
          toggle('performance_setting.monitor_enabled', 'resourceMonitor'),
          {
            ...number(
              'performance_setting.monitor_cpu_threshold',
              'cpuThreshold',
              90
            ),
            min: 0,
            integer: true,
          },
          {
            ...number(
              'performance_setting.monitor_memory_threshold',
              'memoryThreshold',
              90
            ),
            min: 0,
            max: 100,
            integer: true,
          },
          {
            ...number(
              'performance_setting.monitor_disk_threshold',
              'diskThreshold',
              95
            ),
            min: 0,
            max: 100,
            integer: true,
          },
        ],
        integration: 'performance',
      },
      {
        id: 'maintenance',
        titleKey: 'systemSettings.sections.maintenance',
        descriptionKey: 'systemSettings.sections.maintenanceDescription',
        fields: [],
        integration: 'operations-maintenance',
      },
    ],
  },
]

export function getSystemSettingsDomain(
  id: string
): SystemSettingsDomain | undefined {
  return SYSTEM_SETTINGS_DOMAINS.find((domain) => domain.id === id)
}
