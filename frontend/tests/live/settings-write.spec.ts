import { createServer } from 'node:http'
import { expect, test } from '@playwright/test'
import { loginCandidate } from './session'

test('native settings save prerequisites and discover OIDC before writing', async ({
  page,
}) => {
  // A real local identity-provider service. No application API is intercepted.
  const events: string[] = []
  const server = createServer((request, response) => {
    events.push(`provider:${request.url}`)
    if (request.url !== '/discovery') {
      response.writeHead(503).end('Candidate discovery unavailable')
      return
    }
    const origin = `http://127.0.0.1:${(server.address() as { port: number }).port}`
    response.setHeader('Content-Type', 'application/json')
    response.end(
      JSON.stringify({
        issuer: origin,
        authorization_endpoint: `${origin}/authorize`,
        token_endpoint: `${origin}/token`,
        userinfo_endpoint: `${origin}/userinfo`,
      })
    )
  })
  await new Promise<void>((resolve, reject) => {
    server.once('error', reject)
    server.listen(0, '127.0.0.1', resolve)
  })
  const origin = `http://127.0.0.1:${(server.address() as { port: number }).port}`
  const writes: string[] = []
  const results: Array<Promise<boolean>> = []
  page.on('request', (request) => {
    if (
      new URL(request.url()).pathname === '/api/option/' &&
      request.method() === 'PUT'
    ) {
      const key = String(request.postDataJSON().key)
      writes.push(key)
      events.push(`option:${key}`)
    }
  })
  page.on('response', (response) => {
    if (
      new URL(response.url()).pathname === '/api/option/' &&
      response.request().method() === 'PUT'
    )
      results.push(response.json().then((value) => value.success === true))
  })
  await page.addInitScript(() => localStorage.setItem('ren2hub_locale', 'en'))
  try {
    await loginCandidate(page)
    await page.goto('/system-settings/auth/oauth')
    const github = page.getByRole('switch', {
      name: 'Enable GitHub sign-in',
      exact: true,
    })
    const oidc = page.getByRole('switch', {
      name: 'Enable OIDC sign-in',
      exact: true,
    })
    const save = page.getByRole('button', { name: 'Save changes', exact: true })
    const saveThrough = async (lastKey: string) => {
      const confirmed = page.waitForResponse(
        (response) =>
          new URL(response.url()).pathname === '/api/option/' &&
          response.request().method() === 'PUT' &&
          response.request().postDataJSON().key === lastKey
      )
      await save.click()
      expect((await (await confirmed).json()).success).toBe(true)
      await expect(save).toBeDisabled()
    }
    await expect(github).toHaveAttribute('aria-checked', 'false')
    await expect(oidc).toHaveAttribute('aria-checked', 'false')
    await page.locator('[id="setting-GitHubClientId"]').fill('candidate-client')
    await page
      .locator('[id="setting-GitHubClientSecret"]')
      .fill('candidate-provider-secret')
    await github.click()
    await saveThrough('GitHubOAuthEnabled')
    expect(writes).toEqual([
      'GitHubClientId',
      'GitHubClientSecret',
      'GitHubOAuthEnabled',
    ])
    await expect(page.locator('[id="setting-GitHubClientSecret"]')).toHaveValue(
      ''
    )
    await page.reload()
    await expect(github).toHaveAttribute('aria-checked', 'true')
    await expect(page.locator('[id="setting-GitHubClientId"]')).toHaveValue(
      'candidate-client'
    )

    await page.locator('[id="setting-oidc.client_id"]').fill('candidate-oidc')
    await page
      .locator('[id="setting-oidc.client_secret"]')
      .fill('candidate-oidc-secret')
    await page
      .locator('[id="setting-oidc.well_known"]')
      .fill(`${origin}/unavailable`)
    await oidc.click()
    writes.length = 0
    await save.click()
    await expect(page.getByRole('alert')).toBeVisible()
    expect(writes).toEqual([])
    await expect(page.locator('[id="setting-oidc.client_secret"]')).toHaveValue(
      'candidate-oidc-secret'
    )
    await page
      .locator('[id="setting-oidc.well_known"]')
      .fill(`${origin}/discovery`)
    events.length = 0
    await saveThrough('oidc.enabled')
    expect(events[0]).toBe('provider:/discovery')
    expect(writes.at(-1)).toBe('oidc.enabled')
    for (const key of [
      'authorization_endpoint',
      'token_endpoint',
      'user_info_endpoint',
    ])
      expect(writes).toContain(`oidc.${key}`)
    await page.reload()
    await expect(oidc).toHaveAttribute('aria-checked', 'true')
    await expect(
      page.locator('[id="setting-oidc.authorization_endpoint"]')
    ).toHaveValue(`${origin}/authorize`)
    expect((await Promise.all(results)).every(Boolean)).toBe(true)

    // A closed provider must not fetch discovery, even when its saved URL fails.
    await oidc.click()
    await github.click()
    await page
      .locator('[id="setting-oidc.well_known"]')
      .fill(`${origin}/unavailable`)
    events.length = 0
    writes.length = 0
    await saveThrough('oidc.well_known')
    expect(events.every((event) => !event.startsWith('provider:'))).toBe(true)
    expect(writes.slice(0, 2).sort()).toEqual([
      'GitHubOAuthEnabled',
      'oidc.enabled',
    ])
    expect((await Promise.all(results)).every(Boolean)).toBe(true)
  } finally {
    await new Promise<void>((resolve, reject) =>
      server.close((error) => (error ? reject(error) : resolve()))
    )
  }
})
