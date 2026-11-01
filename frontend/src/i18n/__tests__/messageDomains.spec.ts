import { describe, expect, it } from 'vitest'

import enAuth from '@/i18n/locales/en/auth'
import enCommon from '@/i18n/locales/en/common'
import enConsole from '@/i18n/locales/en/console'
import enHome from '@/i18n/locales/en/home'
import enSetup from '@/i18n/locales/en/setup'
import zhAuth from '@/i18n/locales/zh-CN/auth'
import zhCommon from '@/i18n/locales/zh-CN/common'
import zhConsole from '@/i18n/locales/zh-CN/console'
import zhHome from '@/i18n/locales/zh-CN/home'
import zhSetup from '@/i18n/locales/zh-CN/setup'

/**
 * `loadMessageDomain` deep-merges auth/console/setup into the eager home and
 * common messages and never unloads them. A key that two domains define with
 * different values therefore renders whichever domain loaded last: the home
 * navbar CTA used to switch from "进入控制台" to "控制台" as soon as the router
 * began loading the console chunk, and kept the console text after returning.
 * Domains may repeat a key only with the same value.
 */

type Messages = Record<string, unknown>

const locales: Record<string, Record<string, Messages>> = {
  en: {
    home: enHome,
    common: enCommon,
    auth: enAuth,
    console: enConsole,
    setup: enSetup,
  },
  'zh-CN': {
    home: zhHome,
    common: zhCommon,
    auth: zhAuth,
    console: zhConsole,
    setup: zhSetup,
  },
}

function flatten(messages: Messages, prefix = ''): [string, unknown][] {
  return Object.entries(messages).flatMap(([key, value]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return value !== null && typeof value === 'object' && !Array.isArray(value)
      ? flatten(value as Messages, path)
      : [[path, value] as [string, unknown]]
  })
}

function conflictingKeys(domains: Record<string, Messages>): string[] {
  const owners = new Map<string, { domain: string; value: string }>()
  const conflicts: string[] = []
  for (const [domain, messages] of Object.entries(domains)) {
    for (const [key, value] of flatten(messages)) {
      const serialized = JSON.stringify(value)
      const owner = owners.get(key)
      if (!owner) owners.set(key, { domain, value: serialized })
      else if (owner.value !== serialized)
        conflicts.push(
          `${key}: ${owner.domain}=${owner.value} ${domain}=${serialized}`
        )
    }
  }
  return conflicts
}

describe('message domains', () => {
  it.each(Object.keys(locales))(
    'never give one key two values in %s',
    (locale) => {
      expect(conflictingKeys(locales[locale])).toEqual([])
    }
  )
})
