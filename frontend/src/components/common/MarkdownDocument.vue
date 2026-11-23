<script setup lang="ts">
import { computed, h, type VNodeChild, type FunctionalComponent } from 'vue'
import { Lexer, type Token, type Tokens } from 'marked'

// Markdown from remote sources (plugin READMEs, GitHub release notes) is
// rendered as Vue nodes: raw HTML and images are dropped and links keep only
// http, https and mailto targets.
const props = defineProps<{
  markdown: string
  /** Resolves relative links; without it they render as plain link text. */
  baseUrl?: string
}>()

const SAFE_PROTOCOLS = new Set(['http:', 'https:', 'mailto:'])

function safeHref(href: string): string | undefined {
  try {
    const url = new URL(href, props.baseUrl)
    return SAFE_PROTOCOLS.has(url.protocol) ? url.href : undefined
  } catch {
    // A relative link without a base, or an unparsable one, has no target.
    return undefined
  }
}

function render(token: Token): VNodeChild {
  const children =
    'tokens' in token && Array.isArray(token.tokens)
      ? token.tokens.map(render)
      : 'text' in token
        ? String(token.text)
        : ''
  switch (token.type) {
    case 'heading':
      return h(
        `h${Math.min(token.depth + 1, 6)}`,
        { class: 'font-semibold' },
        children
      )
    case 'paragraph':
      return h('p', children)
    case 'code':
      return h(
        'pre',
        {
          class:
            'overflow-auto rounded-lg bg-[var(--surface-muted)] p-3 text-xs',
        },
        h('code', token.text)
      )
    case 'codespan':
      return h('code', { class: 'font-mono' }, token.text)
    case 'strong':
      return h('strong', children)
    case 'em':
      return h('em', children)
    case 'del':
      return h('s', children)
    case 'blockquote':
      return h('blockquote', { class: 'border-l-2 pl-3' }, children)
    case 'br':
      return h('br')
    case 'hr':
      return h('hr')
    case 'list':
      return h(
        token.ordered ? 'ol' : 'ul',
        { class: token.ordered ? 'list-decimal pl-5' : 'list-disc pl-5' },
        token.items.map(render)
      )
    case 'list_item':
      return h('li', children)
    case 'table':
      return renderTable(token as Tokens.Table)
    case 'link':
      return h(
        'a',
        {
          href: safeHref(token.href),
          target: '_blank',
          rel: 'noopener noreferrer',
          class: 'text-[var(--accent)] underline',
        },
        children
      )
    case 'html':
    case 'image':
      return ''
    default:
      return children
  }
}

function renderTable(token: Tokens.Table): VNodeChild {
  return h(
    'div',
    { class: 'overflow-auto' },
    h('table', { class: 'w-full text-left' }, [
      h(
        'thead',
        h(
          'tr',
          token.header.map((cell) =>
            h('th', { class: 'p-2' }, cell.tokens.map(render))
          )
        )
      ),
      h(
        'tbody',
        token.rows.map((row) =>
          h(
            'tr',
            row.map((cell) =>
              h(
                'td',
                { class: 'border-t border-[var(--border-subtle)] p-2' },
                cell.tokens.map(render)
              )
            )
          )
        )
      ),
    ])
  )
}

const tokens = computed(() => Lexer.lex(props.markdown))
const Document: FunctionalComponent = () => tokens.value.map(render)
</script>
<template>
  <div class="sibling-space-y-3 break-words text-sm"><Document /></div>
</template>
