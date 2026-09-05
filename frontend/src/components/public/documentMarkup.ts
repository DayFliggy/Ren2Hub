import { marked } from 'marked'

export function publicDocumentUrl(content: string): string | null {
  if (!/^https?:\/\/\S+$/i.test(content.trim())) return null
  try {
    const url = new URL(content.trim())
    return url.username || url.password ? null : url.href
  } catch {
    return null
  }
}

export function publicDocumentMarkup(
  content: string,
  theme: {
    background: string
    foreground: string
    accent: string
    scheme: string
  }
): string {
  const html = marked.parse(content, { async: false })
  const document = new DOMParser().parseFromString(html, 'text/html')
  // Documents are isolated in a scriptless sandbox; remove navigation and active embeds as well.
  document
    .querySelectorAll(
      'script, iframe, frame, object, embed, base, meta, form, link'
    )
    .forEach((element) => element.remove())
  for (const element of document.querySelectorAll('*')) {
    for (const attribute of [...element.attributes]) {
      if (
        attribute.name.toLowerCase().startsWith('on') ||
        attribute.name.toLowerCase() === 'style'
      )
        element.removeAttribute(attribute.name)
    }
  }
  for (const anchor of document.querySelectorAll('a')) {
    const href = anchor.getAttribute('href') ?? ''
    if (!/^(https?:\/\/|mailto:|#|\/[^/])/i.test(href))
      anchor.removeAttribute('href')
    anchor.setAttribute('target', '_blank')
    anchor.setAttribute('rel', 'noopener noreferrer')
  }
  const style = document.createElement('style')
  style.textContent = `:root{color-scheme:${theme.scheme}}body{margin:0;padding:20px;background:${theme.background};color:${theme.foreground};font:16px/1.75 system-ui,sans-serif;overflow-wrap:anywhere}a{color:${theme.accent}}img,video{max-width:100%;height:auto}pre{overflow:auto}table{display:block;max-width:100%;overflow:auto;border-collapse:collapse}td,th{padding:8px;border:1px solid currentColor}h1,h2,h3{line-height:1.3}*{box-sizing:border-box}`
  document.head.append(style)
  const csp = document.createElement('meta')
  csp.setAttribute('http-equiv', 'Content-Security-Policy')
  csp.setAttribute(
    'content',
    "default-src 'none'; img-src https: http: data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"
  )
  document.head.prepend(csp)
  return `<!doctype html>${document.documentElement.outerHTML}`
}
