import { describe, expect, it } from 'vitest'
import { SYSTEM_SETTINGS_DOMAINS } from '@/constants/systemSettingsCatalog'

describe('system settings catalog', () => {
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
    expect(keys).toContain('ModelRequestRateLimitGroup')
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
    ).toBe('ratio')
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
      'payment_setting.epay_settlement_enabled',
      'Price',
      'MinTopUp',
      'PayMethods',
    ])
    expect(gmpay.fields.map((field) => field.key)).toEqual([
      'payment_setting.gmpay_settlement_enabled',
    ])
    expect(gmpay.managedKeys).toEqual([
      'GmpayPayAddress',
      'GmpayId',
      'GmpayKey',
      'GmpayAssets',
      'GmpayPrice',
      'GmpayMinTopUp',
      'GmpayGroupRatio',
    ])
    expect(epay.fields.every((field) => !!field.labelKey)).toBe(true)
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

  it('lists model pricing once, as a billing section', () => {
    const owners = SYSTEM_SETTINGS_DOMAINS.flatMap((domain) =>
      domain.sections
        .filter((section) => section.integration === 'model-pricing')
        .map((section) => `${domain.id}/${section.id}`)
    )
    expect(owners).toEqual(['billing/model-pricing'])
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
})
