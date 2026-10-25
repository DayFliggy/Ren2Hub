import { expect, test, type Locator, type Page } from '@playwright/test'
import commonEn from '../../src/i18n/locales/en/common'
import commonZh from '../../src/i18n/locales/zh-CN/common'
import consoleEn from '../../src/i18n/locales/en/console'
import consoleZh from '../../src/i18n/locales/zh-CN/console'
import {
  assertNoHorizontalOverflow,
  configureStablePage,
  waitForStablePage,
} from './fixtures'
import { captureUI } from './ui-coverage'

// controller/token.go accepts at most 50 bytes, and KeyFormModal uses maxlength=50.
// ASCII makes this boundary independent of UTF-8 code-point width.
const longName = `Atlas-${'W'.repeat(44)}`
const atlasNames = Array.from({ length: 13 }, (_, index) =>
  index === 0 ? longName : `Atlas-${String(index + 1).padStart(2, '0')}`
)
const otherNames = Array.from(
  { length: 13 },
  (_, index) => `Other-${String(index + 1).padStart(2, '0')}`
)
const allNames = atlasNames.flatMap((name, index) => [name, otherNames[index]!])

interface ListRead {
  path: string
  page: number
  size: number
  keyword: string
  total: number
  names: string[]
}

async function paginatedTokenFixture(page: Page) {
  const reads: ListRead[] = []
  const tokens = allNames.map((name, index) => ({
    id: index + 101,
    name,
    key: `sk-****${String(index + 101).padStart(4, '0')}`,
    type: 'auto',
    status: 1,
    remain_quota: 500000,
    used_quota: 0,
    unlimited_quota: false,
    model_limits_enabled: false,
    model_limits: '',
    allow_ips: null,
    allow_user_quota_query: true,
    created_time: 1785124800,
    expired_time: -1,
  }))
  await page.route(
    (url) => ['/api/token/', '/api/token/search'].includes(url.pathname),
    async (route) => {
      if (route.request().method() !== 'GET') return route.fallback()
      const url = new URL(route.request().url())
      const currentPage = Number(url.searchParams.get('p'))
      const size = Number(url.searchParams.get('page_size'))
      const keyword = url.searchParams.get('keyword') ?? ''
      expect(Number.isInteger(currentPage) && currentPage >= 1).toBe(true)
      expect([10, 20, 50]).toContain(size)
      const matching = tokens.filter((token) => token.name.includes(keyword))
      const items = matching.slice((currentPage - 1) * size, currentPage * size)
      reads.push({
        path: url.pathname,
        page: currentPage,
        size,
        keyword,
        total: matching.length,
        names: items.map((item) => item.name),
      })
      await route.fulfill({
        json: {
          success: true,
          data: {
            page: currentPage,
            page_size: size,
            total: matching.length,
            items,
          },
        },
      })
    }
  )
  return reads
}

const scenarios = [
  {
    name: 'desktop-light-zh-CN',
    viewport: { width: 1440, height: 900 },
    theme: 'light',
    locale: 'zh-CN',
    mobile: false,
  },
  {
    name: 'mobile-dark-en',
    viewport: { width: 390, height: 844 },
    theme: 'dark',
    locale: 'en',
    mobile: true,
  },
] as const

for (const scenario of scenarios) {
  test(`pagination selection and filter ${scenario.name}`, async ({
    page,
  }, info) => {
    const common = (scenario.locale === 'en' ? commonEn : commonZh).common
    const keys = (scenario.locale === 'en' ? consoleEn : consoleZh).keys
    await page.setViewportSize(scenario.viewport)
    const native = await configureStablePage(page, scenario)
    const reads = await paginatedTokenFixture(page)
    await page.goto('/keys')
    await waitForStablePage(page)

    const list = page.locator(
      scenario.mobile ? '.key-list-cards' : '.key-list-table'
    )
    await expect(list).toBeVisible()
    const rows = list.locator(
      scenario.mobile ? '[data-key-mobile-row]' : 'tbody tr'
    )
    const names = list.locator(
      scenario.mobile
        ? '[data-key-mobile-row] h2'
        : 'tbody tr td:nth-child(2) > span'
    )
    const pagination = list.locator('[data-handdrawn="pagination"]')
    const allSelected = list.getByRole('checkbox', {
      name: common.selectAll,
      exact: true,
    })
    const bulk = (count: number) =>
      page.getByRole('button', {
        name: `${keys.batchDelete} (${count})`,
        exact: true,
      })
    const selectedRows = () => rows.locator('input[type="checkbox"]:checked')
    const total = (count: number) =>
      pagination.getByText(common.pageInfo.replace('{total}', String(count)), {
        exact: true,
      })
    const rowCheckbox = (name: string): Locator =>
      rows
        .filter({ has: page.getByText(name, { exact: true }) })
        .getByRole('checkbox')

    await expect(names).toHaveText(allNames.slice(0, 10))
    await expect(total(26)).toBeVisible()
    expect(reads.at(-1)).toMatchObject({
      path: '/api/token/',
      page: 1,
      size: 10,
      keyword: '',
      total: 26,
    })
    await rowCheckbox(longName).check()
    await rowCheckbox('Other-01').check()
    await expect(bulk(2)).toBeVisible()
    await expect(selectedRows()).toHaveCount(2)
    if (scenario.mobile) {
      await expect(names.first()).toHaveAttribute('title', longName)
      await expect(rowCheckbox(longName)).toHaveAccessibleName(
        keys.selectKey.replace('{name}', longName)
      )
    }
    await assertNoHorizontalOverflow(page)
    await bulk(2).scrollIntoViewIfNeeded()
    // State checkpoints keep the interacted toolbar/footer in view; the full
    // normal-state scroll traversal is covered by the independent page matrix.
    await captureUI(page, info, 'long-name-and-two-selected', { scroll: false })

    await page.locator('[name="token-search"]').fill('Atlas')
    await expect(names).toHaveText(atlasNames.slice(0, 10))
    await expect(total(13)).toBeVisible()
    expect(reads.at(-1)).toMatchObject({
      path: '/api/token/search',
      page: 1,
      size: 10,
      keyword: 'Atlas',
      total: 13,
    })
    await expect(bulk(2)).toHaveCount(0)
    await expect(selectedRows()).toHaveCount(0)

    await rowCheckbox('Atlas-02').check()
    await expect(bulk(1)).toBeVisible()
    await pagination
      .getByRole('button', { name: common.nextPage, exact: true })
      .filter({ visible: true })
      .click()
    await expect(names).toHaveText(['Atlas-11', 'Atlas-12', 'Atlas-13'])
    await expect(total(13)).toBeVisible()
    expect(reads.at(-1)).toMatchObject({
      path: '/api/token/search',
      page: 2,
      size: 10,
      keyword: 'Atlas',
      names: ['Atlas-11', 'Atlas-12', 'Atlas-13'],
    })
    // Keys selections belong to the loaded page; changing the server result
    // clears selection rather than retaining IDs from the previous page.
    await expect(bulk(1)).toHaveCount(0)
    await expect(selectedRows()).toHaveCount(0)
    await allSelected.check()
    await expect(selectedRows()).toHaveCount(3)
    await expect(bulk(3)).toBeVisible()
    await rowCheckbox('Atlas-12').uncheck()
    await expect(allSelected).not.toBeChecked()
    await expect(selectedRows()).toHaveCount(2)
    await expect(bulk(2)).toBeVisible()
    await allSelected.check()
    await expect(bulk(3)).toBeVisible()
    await bulk(3).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toContainText(
      keys.batchDeleteMessage.replace('{count}', '3')
    )
    await captureUI(page, info, 'filtered-page-two-selection-confirmation', {
      scroll: false,
    })
    await dialog
      .getByRole('button', { name: keys.thinkAgain, exact: true })
      .click()
    await expect(dialog).toHaveCount(0)
    await expect(selectedRows()).toHaveCount(3)

    await pagination
      .getByRole('combobox', { name: common.itemsPerPage, exact: true })
      .click()
    await page
      .getByRole('option', {
        name: common.pageSize.replace('{size}', '20'),
        exact: true,
      })
      .click()
    await expect(names).toHaveText(atlasNames)
    await expect(total(13)).toBeVisible()
    expect(reads.at(-1)).toMatchObject({
      path: '/api/token/search',
      page: 1,
      size: 20,
      keyword: 'Atlas',
      total: 13,
      names: atlasNames,
    })
    await expect(selectedRows()).toHaveCount(0)
    await expect(bulk(3)).toHaveCount(0)
    await expect(
      pagination.getByRole('button', { name: common.nextPage, exact: true })
    ).toHaveCount(0)
    await pagination.scrollIntoViewIfNeeded()
    await assertNoHorizontalOverflow(page)
    await captureUI(page, info, 'page-size-twenty-total-thirteen', {
      scroll: false,
    })

    await page.locator('[name="token-search"]').fill('')
    await expect(names).toHaveText(allNames.slice(0, 20))
    await expect(total(26)).toBeVisible()
    expect(reads.at(-1)).toMatchObject({
      path: '/api/token/',
      page: 1,
      size: 20,
      keyword: '',
      total: 26,
    })
    expect(
      native.writes.filter(
        (request) => request.path !== '/api/user/auth/refresh'
      )
    ).toEqual([])
    await info.attach('pagination-requests-and-results', {
      body: JSON.stringify(reads, null, 2),
      contentType: 'application/json',
    })
    await waitForStablePage(page)
  })
}
