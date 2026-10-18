import { describe, expect, it } from 'vitest'
import { SYSTEM_SETTINGS_DOMAINS } from '@/constants/systemSettingsCatalog'
import enConsole from '@/i18n/locales/en/console'
import zhConsole from '@/i18n/locales/zh-CN/console'

function message(messages: unknown, key: unknown): unknown {
  if (typeof key !== 'string') return undefined
  return key
    .split('.')
    .reduce<unknown>(
      (node, part) =>
        node && typeof node === 'object'
          ? (node as Record<string, unknown>)[part]
          : undefined,
      messages
    )
}

describe('system settings catalog', () => {
  it('matches the server limits for referral hold and eligibility periods', () => {
    const fields = SYSTEM_SETTINGS_DOMAINS.flatMap((domain) =>
      domain.sections.flatMap((section) => section.fields)
    )
    expect(
      fields.find((field) => field.key === 'affiliate_setting.freeze_hours')
    ).toMatchObject({ min: 0, max: 720, integer: true })
    expect(
      fields.find((field) => field.key === 'affiliate_setting.duration_days')
    ).toMatchObject({ min: 0, max: 3650, integer: true })
  })
  it('provides a stable default deep link for every top-level domain', () => {
    expect(SYSTEM_SETTINGS_DOMAINS.map((domain) => domain.id)).toEqual([
      'site',
      'auth',
      'billing',
      'models',
      'security',
      'content',
      'operations',
    ])

    for (const domain of SYSTEM_SETTINGS_DOMAINS) {
      expect(
        domain.sections.some((section) => section.id === domain.defaultSection)
      ).toBe(true)
    }
  })

  it('maps each option key to one visible administration section', () => {
    const keys = SYSTEM_SETTINGS_DOMAINS.flatMap((domain) =>
      domain.sections.flatMap((section) => [
        ...section.fields.map((field) => field.key),
        ...(section.managedKeys ?? []),
      ])
    )

    expect(new Set(keys).size).toBe(keys.length)
    expect(keys).toContain('HeaderNavModules')
    expect(keys).not.toContain('ModelRequestRateLimitGroup')
    expect(keys).not.toContain('quota_setting.trust_quota_usd')
    expect(keys).toContain('quota_setting.pre_consume_multiplier')
    expect(keys).toContain('WorkerAllowHttpImageRequestEnabled')
    expect(keys).toContain('perf_metrics_setting.retention_days')
    expect(keys).not.toContain('billing_setting.billing_expr')
    expect(keys).toContain('payment_setting.amount_discount')
    expect(keys).toContain('channel_affinity_setting.rules')
    expect(keys).toContain('global.chat_completions_to_responses_policy')
    expect(keys).toContain('FileUploadPermission')
    expect(keys).not.toContain('grok.reasoning_effort')
    expect(keys).toContain('WaffoPancakeUnitPrice')
  })

  it('uses structured editors for pricing, lists, and Waffo Pancake setup', () => {
    const sections = SYSTEM_SETTINGS_DOMAINS.flatMap(
      (domain) => domain.sections
    )
    const payment = sections.find((section) => section.id === 'payment')
    const ssrf = sections.find((section) => section.id === 'ssrf')
    const waffoPancake = sections.find(
      (section) => section.id === 'waffo-pancake'
    )

    expect(
      payment?.fields.find((field) => field.key === 'TopupGroupRatio')?.kind
    ).toBeUndefined()
    expect(
      ssrf?.fields.find((field) => field.key === 'fetch_setting.domain_list')
        ?.kind
    ).toBe('list')
    expect(waffoPancake?.integration).toBe('waffo-pancake')
  })

  it('exposes EPay and GMPay contracts in distinct settings pages without duplicate keys', () => {
    const sections = SYSTEM_SETTINGS_DOMAINS.find(
      (domain) => domain.id === 'billing'
    )!.sections
    const epay = sections.find((section) => section.id === 'epay')!
    const gmpay = sections.find((section) => section.id === 'gmpay')!
    expect(epay.fields.map((field) => field.key)).toEqual([
      'PayAddress',
      'EpayId',
      'EpayKey',
      'Price',
      'MinTopUp',
      'PayMethods',
    ])
    expect(gmpay.fields).toEqual([])
    expect(gmpay.managedKeys).toEqual([
      'GmpayPayAddress',
      'GmpayId',
      'GmpayKey',
      'GmpayAssets',
      'GmpayPrice',
      'GmpayMinTopUp',
    ])
    const payment = sections.find((section) => section.id === 'payment')!
    expect(
      payment.fields.some((field) => field.key === 'StripeApiSecret')
    ).toBe(true)
    expect(payment.fields.some((field) => field.key === 'CreemProducts')).toBe(
      true
    )
    expect(
      payment.fields.some((field) => field.key === 'WaffoSandboxPublicCert')
    ).toBe(true)
  })

  it('names every domain, section, field and option in both locales', () => {
    const keys = SYSTEM_SETTINGS_DOMAINS.flatMap((domain) => [
      domain.titleKey,
      ...domain.sections.flatMap((section) => [
        section.titleKey,
        section.descriptionKey,
        ...section.fields.flatMap((field) => [
          field.labelKey,
          ...(field.descriptionKey ? [field.descriptionKey] : []),
          ...(field.options ?? []).map((option) => option.labelKey),
        ]),
      ]),
    ])
    for (const messages of [zhConsole, enConsole]) {
      const missing = keys.filter((key) => {
        const text = message(messages, key)
        return typeof text !== 'string' || !text.trim()
      })
      expect(missing).toEqual([])
    }
  })

  it('keeps model pricing out of system settings', () => {
    const owners = SYSTEM_SETTINGS_DOMAINS.flatMap((domain) =>
      domain.sections
        .filter((section) => section.id === 'model-pricing')
        .map((section) => `${domain.id}/${section.id}`)
    )
    expect(owners).toEqual([])
  })

  it('excludes retired automatic pricing options and editable deployment settings', () => {
    const sections = SYSTEM_SETTINGS_DOMAINS.flatMap(
      (domain) => domain.sections
    )
    expect(
      sections
        .flatMap((section) => section.fields)
        .some(
          (field) =>
            field.key.startsWith('model_deployment.') ||
            field.key.startsWith('auto_pricing.') ||
            field.key === 'ExposeRatioEnabled'
        )
    ).toBe(false)
  })
  it('exposes the Gemini thought-signature bypass enabled by default', () => {
    const field = SYSTEM_SETTINGS_DOMAINS.flatMap((domain) =>
      domain.sections.flatMap((section) => section.fields)
    ).find(
      (candidate) =>
        candidate.key === 'gemini.function_call_thought_signature_enabled'
    )
    expect(field?.kind).toBe('boolean')
    expect(field?.defaultValue).toBe(true)
  })
})
