import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  fetchPluginChangelog,
  parsePluginChangelog,
} from '../lib/plugin-changelog'
import { localizedText } from '@/utils/localizedText'
import type { MarketplacePlugin } from '../types'

const indexUrl = 'https://plugins.example/index.json'
const plugin: MarketplacePlugin = {
  key: 'fixture',
  name: 'Fixture',
  description: '',
  latest: '1.0.0',
  versions: [
    {
      version: '1.0.0',
      path: 'fixture/1.0.0/plugin.js',
      sha256: 'a'.repeat(64),
    },
  ],
}
const source = 'https://plugins.example/fixture/1.0.0/CHANGELOG.md'
function notes(locale = 'en', body = '- Fixed request handling.') {
  return `---\nchangelogVersion: 1\nplugin: fixture\nversion: 1.0.0\nlocale: ${locale}\n${locale === 'en' ? 'translations:\n  zh-CN: CHANGELOG.zh-CN.md\n' : ''}---\n# Changelog\n## [1.0.0]\n### Fixed\n${body}\n`
}
afterEach(() => vi.restoreAllMocks())
describe('plugin changelog integrity and localization', () => {
  it('uses primary language and English description fallback', () => {
    expect(localizedText({ en: 'English', zh: '中文' }, 'zh-CN')).toBe('中文')
    expect(localizedText({ en: 'English', zh: '中文' }, 'fr')).toBe('English')
  })
  it('loads the declared translation without browser credentials', async () => {
    const fetcher = vi
      .spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(notes()))
      .mockResolvedValueOnce(new Response(notes('zh-CN', '- 修复请求处理。')))
    const entry = await fetchPluginChangelog({
      indexUrl,
      plugin,
      version: '1.0.0',
      language: 'zh-CN',
      signal: new AbortController().signal,
    })
    expect(entry?.locale).toBe('zh-CN')
    expect(entry?.sections[0]?.markdown).toContain('修复请求处理')
    expect(fetcher).toHaveBeenLastCalledWith(
      source.replace('.md', '.zh-CN.md'),
      expect.objectContaining({ credentials: 'omit', redirect: 'error' })
    )
  })
  it('falls back to validated English when a declared translation is unavailable', async () => {
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(new Response(notes()))
      .mockResolvedValueOnce(new Response('', { status: 404 }))
    const entry = await fetchPluginChangelog({
      indexUrl,
      plugin,
      version: '1.0.0',
      language: 'zh-CN',
      signal: new AbortController().signal,
    })
    expect(entry?.locale).toBe('en')
  })
  it('rejects mismatched releases and active embedded content', () => {
    expect(() =>
      parsePluginChangelog(
        notes(),
        { plugin: 'other', version: '1.0.0', locale: 'en' },
        source
      )
    ).toThrow('identity')
    expect(() =>
      parsePluginChangelog(
        notes('en', '- <script>bad()</script>'),
        { plugin: 'fixture', version: '1.0.0', locale: 'en' },
        source
      )
    ).toThrow()
    expect(() =>
      parsePluginChangelog(
        notes().replace('CHANGELOG.zh-CN.md', 'https://evil.test/a.md'),
        { plugin: 'fixture', version: '1.0.0', locale: 'en' },
        source
      )
    ).toThrow('filename')
  })
})
