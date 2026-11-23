import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import MarkdownDocument from '../MarkdownDocument.vue'

describe('markdown document', () => {
  it('renders block and inline markdown as elements', () => {
    const view = mount(MarkdownDocument, {
      props: {
        markdown: [
          '## Changes',
          '',
          'Some **bold**, *soft* and `code` text.',
          '',
          '1. first',
          '2. second',
          '',
          '```',
          'go build ./...',
          '```',
          '',
          '| Name | Value |',
          '| --- | --- |',
          '| a | 1 |',
        ].join('\n'),
      },
    })
    expect(view.get('h3').text()).toBe('Changes')
    expect(view.get('strong').text()).toBe('bold')
    expect(view.get('em').text()).toBe('soft')
    expect(view.get('p code').text()).toBe('code')
    expect(view.findAll('ol li').map((item) => item.text())).toEqual([
      'first',
      'second',
    ])
    expect(view.get('pre code').text()).toBe('go build ./...')
    expect(view.findAll('th').map((cell) => cell.text())).toEqual([
      'Name',
      'Value',
    ])
    expect(view.findAll('td').map((cell) => cell.text())).toEqual(['a', '1'])
  })

  it('resolves relative links and drops unsafe targets, html and images', () => {
    const view = mount(MarkdownDocument, {
      props: {
        baseUrl: 'https://plugins.test/fixture/README.md',
        markdown: [
          '[guide](docs/guide.md) [site](https://example.com/) [mail](mailto:a@example.com)',
          '[script](javascript:alert(1)) [data](data:text/html,hi)',
          '',
          '<script>alert(1)</script>',
          '',
          '<b>raw</b> ![logo](https://example.com/logo.png)',
        ].join('\n'),
      },
    })
    const links = view.findAll('a')
    expect(links.map((link) => link.text())).toEqual([
      'guide',
      'site',
      'mail',
      'script',
      'data',
    ])
    expect(links.map((link) => link.attributes('href'))).toEqual([
      'https://plugins.test/fixture/docs/guide.md',
      'https://example.com/',
      'mailto:a@example.com',
      undefined,
      undefined,
    ])
    expect(links[0]!.attributes()).toMatchObject({
      target: '_blank',
      rel: 'noopener noreferrer',
    })
    expect(view.find('script').exists()).toBe(false)
    expect(view.find('b').exists()).toBe(false)
    expect(view.find('img').exists()).toBe(false)
  })

  it('leaves relative links without a base as plain text', () => {
    const view = mount(MarkdownDocument, {
      props: { markdown: '[guide](docs/guide.md)' },
    })
    expect(view.get('a').attributes('href')).toBeUndefined()
    expect(view.get('a').text()).toBe('guide')
  })
})
