import { expect, test } from '@playwright/test'

test('public transport omits same-origin cookies and never refreshes auth', async ({
  context,
  page,
  baseURL,
}) => {
  await context.addCookies([
    { name: 'transport_test_cookie', value: 'test-only', url: baseURL! },
    { name: 'XSRF-TOKEN', value: 'test-only-xsrf', url: baseURL! },
  ])
  await page.route('**/__transport_fixture', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: '<title>Transport fixture</title>',
    })
  )
  let refreshes = 0
  await page.route('**/api/user/auth/refresh', (route) => {
    refreshes++
    return route.fulfill({ status: 500, body: '{}' })
  })
  const observed: Record<string, string>[] = []
  await page.route('**/api/transport-public-probe', async (route) => {
    observed.push(await route.request().allHeaders())
    await route.fulfill({
      status: 401,
      contentType: 'application/json',
      body: JSON.stringify({ success: false, message: 'test rejection' }),
    })
  })
  await page.goto('/__transport_fixture')
  const status = await page.evaluate(async () => {
    const source = '/src/api/httpTransport.ts'
    const { publicHttpTransport } = await import(source)
    try {
      await publicHttpTransport.request('GET', '/api/transport-public-probe')
      return 200
    } catch (error) {
      return (error as { status?: number }).status
    }
  })
  expect(status).toBe(401)
  expect(observed).toHaveLength(1)
  expect(observed[0]?.cookie).toBeUndefined()
  expect(observed[0]?.authorization).toBeUndefined()
  expect(observed[0]?.['x-xsrf-token']).toBeUndefined()
  expect(refreshes).toBe(0)
})

test('canceling an in-flight authenticated transport request rejects without retry', async ({
  page,
}) => {
  await page.route('**/__transport_fixture', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: '<title>Transport fixture</title>',
    })
  )
  let sends = 0
  await page.route('**/api/transport-cancel-probe', async (route) => {
    sends++
    await page.evaluate(() => {
      const state = window as typeof window & {
        transportAbort: AbortController
      }
      state.transportAbort.abort()
    })
    await route.abort('aborted')
  })
  await page.goto('/__transport_fixture')
  const result = await page.evaluate(async () => {
    const source = '/src/api/httpTransport.ts'
    const { httpTransport } = await import(source)
    const state = window as typeof window & { transportAbort: AbortController }
    state.transportAbort = new AbortController()
    try {
      await httpTransport.request('GET', '/api/transport-cancel-probe', {
        signal: state.transportAbort.signal,
      })
      return { resolved: true }
    } catch (error) {
      return {
        resolved: false,
        code: (error as { cause?: { code?: string } }).cause?.code,
      }
    }
  })
  expect(result).toEqual({ resolved: false, code: 'ERR_CANCELED' })
  expect(sends).toBe(1)
})

test('both transports send the console language as Accept-Language', async ({
  page,
}) => {
  await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
  await page.route('**/__transport_fixture', (route) =>
    route.fulfill({
      contentType: 'text/html',
      body: '<title>Transport fixture</title>',
    })
  )
  const languages: Array<string | undefined> = []
  await page.route('**/api/transport-language-probe', async (route) => {
    languages.push((await route.request().allHeaders())['accept-language'])
    await route.fulfill({ json: { success: true, data: null } })
  })
  await page.goto('/__transport_fixture')
  await page.evaluate(async () => {
    const transportSource = '/src/api/httpTransport.ts'
    const i18nSource = '/src/i18n/index.ts'
    const { httpTransport, publicHttpTransport } = await import(transportSource)
    const { setLocale } = await import(i18nSource)
    const probe = '/api/transport-language-probe'
    await httpTransport.request('GET', probe)
    await publicHttpTransport.request('GET', probe)
    await setLocale('zh-CN')
    await httpTransport.request('GET', probe)
    await publicHttpTransport.request('GET', probe)
  })
  expect(languages).toEqual(['en', 'en', 'zh-CN', 'zh-CN'])
})
