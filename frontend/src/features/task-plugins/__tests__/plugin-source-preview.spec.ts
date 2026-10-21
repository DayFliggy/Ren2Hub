import { describe, expect, it } from 'vitest'
import { previewPluginSource } from '../lib/plugin-source-preview'
import { previewFromServer } from '../lib/plugin-meta-preview'

describe('native plugin metadata preview', () => {
  it('reads literal metadata without executing the rest of a plugin', async () => {
    const result = await previewPluginSource(`
      export const meta = { name: 'demo', models: ['a'], channelTypes: [1],
        protocols: [{name: 'openai', supports: ['sync']}], auth: {type: 'bearer'} };
      throw new Error('must never execute');
    `)
    expect(result.unresolved).toEqual([])
    expect(result.meta.models).toEqual(['a'])
    expect(previewFromServer(result).fields.allowedHosts.state).toBe('missing')
  })

  it('keeps dynamic fields unknown instead of making up empty values', async () => {
    const result = await previewPluginSource(
      `const meta = {models: getModels(), channelTypes: [1]}`
    )
    expect(result.unresolved).toEqual(['models'])
    expect(previewFromServer(result).fields.models.state).toBe('unknown')
    expect(previewFromServer(result).fields.channelTypes.state).toBe('value')
  })

  it.each([
    `const meta = { ...other, models: ['a'] }`,
    `const meta = {models: ['a']}; meta.models.push('b')`,
    `const meta = {models: ['a']}; change(meta)`,
    `function define() { const meta = {models: ['a']} }`,
    `const meta = {['models']: ['a']}`,
    `const meta = {models: ['a'], models: ['b']}`,
    `const meta = {get models() { throw Error() }}`,
    `const meta = {models:`,
  ])('reports ambiguous metadata as unavailable: %s', async (source) => {
    expect(previewFromServer(await previewPluginSource(source)).status).toBe(
      'unavailable'
    )
  })

  it('handles object keys without polluting prototypes', async () => {
    const result = await previewPluginSource(
      `const meta = {'__proto__': {polluted: true}, models: []}`
    )
    expect(Object.getPrototypeOf(result.meta)).toBeNull()
    expect(Object.hasOwn({}, 'polluted')).toBe(false)
  })

  it('honors request cancellation before parsing', async () => {
    const controller = new AbortController()
    controller.abort()
    await expect(
      previewPluginSource('const meta = {}', controller.signal)
    ).rejects.toMatchObject({ name: 'AbortError' })
  })
})
