import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import { api } from '@/api/client'
import {
  listRequestPluginInstances,
  saveRequestPluginInstances,
  dryRunRequestPlugins,
} from '../api'
import PluginConfigEditor from '../components/PluginConfigEditor.vue'
import { parsePluginReadme } from '../lib/plugin-readme'
import { parseMarketplaceIndex } from '../lib/marketplace'
import { resolvePluginSiteUrl, pluginSite } from '../lib/plugin-site'
afterEach(() => {
  vi.restoreAllMocks()
  pluginSite.value = 'ai'
})
describe('request processing and plugin documents', () => {
  it('retains the ordered instance list and sends the dry-run reply', async () => {
    const rows = [
      {
        id: 'second',
        name: 'Second',
        plugin: 'fixture',
        enabled: true,
        global: true,
        models: [],
        config: { mode: 'safe' },
      },
      {
        id: 'first',
        name: 'First',
        plugin: 'fixture',
        enabled: true,
        global: false,
        models: ['x'],
        config: {},
      },
    ]
    const get = vi.spyOn(api, 'get').mockResolvedValue({ instances: rows })
    const put = vi.spyOn(api, 'put').mockResolvedValue({ instances: rows })
    const post = vi
      .spyOn(api, 'post')
      .mockResolvedValue({ steps: [], body: {} })
    expect(await listRequestPluginInstances()).toEqual(rows)
    expect(get).toHaveBeenCalledWith(
      '/api/request_plugin/instances',
      undefined,
      { signal: undefined }
    )
    expect(await saveRequestPluginInstances(rows)).toEqual(rows)
    expect(put).toHaveBeenCalledWith(
      '/api/request_plugin/instances',
      { instances: rows },
      { signal: undefined }
    )
    await dryRunRequestPlugins({
      format: 'openai',
      model: 'x',
      body: { messages: [] },
      reply: 'answer',
      instance: rows[0],
    })
    expect(post.mock.calls[0]![1]).toMatchObject({
      reply: 'answer',
      instance: rows[0],
    })
  })
  it('accepts redacted bind reads but rejects malformed instance shapes', async () => {
    const get = vi.spyOn(api, 'get').mockResolvedValue({
      instances: [
        {
          id: 'bound',
          name: 'Bound',
          plugin: 'fixture',
          enabled: true,
          global: false,
          models: null,
          config: null,
        },
      ],
    })
    await expect(listRequestPluginInstances()).resolves.toEqual([
      {
        id: 'bound',
        name: 'Bound',
        plugin: 'fixture',
        enabled: true,
        global: false,
        models: [],
        config: {},
      },
    ])
    get.mockResolvedValue({ instances: [{ id: 'bound', enabled: 'true' }] })
    await expect(listRequestPluginInstances()).rejects.toThrow(
      'Invalid request plugin instance'
    )
  })

  it('keeps the confirmed plugin scope through a list and save round trip', async () => {
    const scoped = {
      id: 'scoped',
      name: 'Scoped',
      plugin: 'fixture',
      enabled: true,
      global: false,
      models: [],
      config: {},
      plugin_scope: { models: ['claude'], channel_types: [1, 3] },
    }
    const get = vi.spyOn(api, 'get').mockResolvedValue({ instances: [scoped] })
    const put = vi.spyOn(api, 'put').mockResolvedValue({ instances: [scoped] })
    const listed = await listRequestPluginInstances()
    expect(listed).toEqual([scoped])
    await saveRequestPluginInstances(listed)
    expect(put.mock.calls[0]![1]).toEqual({ instances: [scoped] })

    get.mockResolvedValue({
      instances: [
        { ...scoped, plugin_scope: { models: [], channel_types: ['1'] } },
      ],
    })
    await expect(listRequestPluginInstances()).rejects.toThrow(
      'Invalid request plugin instance'
    )
  })

  it('rejects malformed JSON without replacing the last good config', async () => {
    const wrapper = mount(PluginConfigEditor, {
      props: { modelValue: { known: true } },
      global: {
        plugins: [
          createI18n({
            legacy: false,
            locale: 'en',
            missingWarn: false,
            fallbackWarn: false,
          }),
        ],
      },
    })
    await wrapper.get('textarea').setValue('{')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([false])
    await wrapper.get('textarea').setValue('{"known":false}')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([
      { known: false },
    ])
    expect(wrapper.emitted('validity')?.at(-1)).toEqual([true])
    wrapper.unmount()
  })
  it('rejects active Markdown content including nested HTML and dangerous links', () => {
    for (const body of [
      '# Guide\n<script>alert(1)</script>',
      '# Guide\n- ![secret](https://beacon.test)',
      '# Guide\n[Run](javascript:alert(1))',
    ])
      expect(() =>
        parsePluginReadme(body, 'en', 'https://plugins.test/README.md')
      ).toThrow()
    expect(
      parsePluginReadme(
        '# Guide\n[Docs](./guide.md)',
        'en',
        'https://plugins.test/README.md'
      ).markdown
    ).toContain('./guide.md')
  })
  it('sorts community recommendations and recent installs and preserves revocations', () => {
    const plugin = (key: string, extra = {}) => ({
      key,
      name: key,
      latest: '1',
      versions: [{ version: '1', path: key + '.js' }],
      ...extra,
    })
    const index = parseMarketplaceIndex({
      indexVersion: 1,
      plugins: [
        plugin('popular', { installs30d: 20 }),
        plugin('recommended', { recommended: true }),
        plugin('other'),
      ],
      revoked: [
        { sha256: 'A'.repeat(64), reason: { en: 'unsafe' } },
        { sha256: 'bad' },
      ],
    })
    expect(index.plugins.map((p) => p.key)).toEqual([
      'recommended',
      'popular',
      'other',
    ])
    expect(index.revoked).toEqual([
      { sha256: 'a'.repeat(64), key: undefined, reason: { en: 'unsafe' } },
    ])
  })
  it('switches only the built-in site and rejects credentials', () => {
    pluginSite.value = 'pro'
    expect(
      resolvePluginSiteUrl('https://www.newapi.ai/api/v1/plugins/index.json')
    ).toBe('https://www.newapi.pro/api/v1/plugins/index.json')
    expect(resolvePluginSiteUrl('https://custom.test/index.json')).toBe(
      'https://custom.test/index.json'
    )
    expect(() =>
      resolvePluginSiteUrl('https://user:password@custom.test/')
    ).toThrow()
  })
})
