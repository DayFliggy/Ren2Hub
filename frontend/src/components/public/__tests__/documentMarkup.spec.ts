import { describe, expect, it } from 'vitest'
import { publicDocumentMarkup, publicDocumentUrl } from '../documentMarkup'

const theme = {
  background: '#fff',
  foreground: '#111',
  accent: '#06c',
  scheme: 'light',
}

describe('public Markdown document isolation', () => {
  it('renders Markdown while removing active HTML and unsafe navigation', () => {
    const markup = publicDocumentMarkup(
      `# Service terms

| Plan | Price |
| --- | --- |
| Standard | 10 |

[Documentation](https://example.test/docs)

<script>window.parent.location = '/logout'</script>
<iframe src="https://example.test/embed"></iframe>
<form action="https://example.test"><input name="secret"></form>
<meta http-equiv="refresh" content="0;url=https://example.test">
<a href="javascript:alert(1)" onclick="alert(2)" style="position:fixed">Unsafe</a>
<img src="https://example.test/logo.png" onerror="alert(3)">`,
      theme
    )
    const document = new DOMParser().parseFromString(markup, 'text/html')
    expect(document.querySelector('h1')?.textContent).toBe('Service terms')
    expect(document.querySelector('table tbody td')?.textContent).toBe(
      'Standard'
    )
    expect(
      document.querySelector('script, iframe, form, [onclick], [onerror]')
    ).toBeNull()
    expect(document.querySelector('meta[http-equiv="refresh"]')).toBeNull()
    const [safe, unsafe] = [...document.querySelectorAll('a')]
    expect(safe?.getAttribute('href')).toBe('https://example.test/docs')
    expect(safe?.getAttribute('rel')).toBe('noopener noreferrer')
    expect(unsafe?.hasAttribute('href')).toBe(false)
    expect(unsafe?.hasAttribute('style')).toBe(false)
    expect(
      document
        .querySelector('meta[http-equiv="Content-Security-Policy"]')
        ?.getAttribute('content')
    ).toContain("default-src 'none'")
    expect(
      document
        .querySelector('meta[http-equiv="Content-Security-Policy"]')
        ?.getAttribute('content')
    ).not.toContain('script-src')
  })

  it('only embeds HTTP documents without URL credentials', () => {
    expect(publicDocumentUrl(' https://example.test/terms ')).toBe(
      'https://example.test/terms'
    )
    expect(
      publicDocumentUrl('https://name:secret@example.test/terms')
    ).toBeNull()
    expect(publicDocumentUrl('javascript:alert(1)')).toBeNull()
    expect(publicDocumentUrl('# Terms\nText')).toBeNull()
  })
})
