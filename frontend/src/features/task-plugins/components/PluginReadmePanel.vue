<script setup lang="ts">
import {
  computed,
  h,
  onBeforeUnmount,
  ref,
  watch,
  type VNodeChild,
  type FunctionalComponent,
} from 'vue'
import { useI18n } from 'vue-i18n'
import { Lexer, type Token, type Tokens } from 'marked'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import type { MarketplacePlugin } from '../types'
import { fetchPluginReadme, type PluginReadme } from '../lib/plugin-readme'
const props = defineProps<{
  plugin: MarketplacePlugin
  indexUrl: string
  version: string
}>()
const { t, locale } = useI18n()
const readme = ref<PluginReadme | null>(null)
const error = ref('')
let controller: AbortController | undefined
async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  readme.value = null
  error.value = ''
  try {
    const result = await fetchPluginReadme({
      indexUrl: props.indexUrl,
      plugin: props.plugin,
      version: props.version,
      language: locale.value,
      signal: request.signal,
    })
    if (!request.signal.aborted) readme.value = result
  } catch (cause) {
    if (!request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  }
}
watch(
  () => [props.plugin.key, props.indexUrl, props.version, locale.value],
  load,
  { immediate: true }
)
onBeforeUnmount(() => controller?.abort())
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
      return h(
        'div',
        { class: 'overflow-auto' },
        h('table', { class: 'w-full text-left' }, [
          h(
            'thead',
            h(
              'tr',
              token.header.map((cell: Tokens.TableCell) =>
                h('th', { class: 'p-2' }, cell.tokens.map(render))
              )
            )
          ),
          h(
            'tbody',
            token.rows.map((row: Tokens.TableCell[]) =>
              h(
                'tr',
                row.map((cell: Tokens.TableCell) =>
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
    case 'link':
      return h(
        'a',
        {
          href: new URL(token.href, readme.value?.sourceUrl).href,
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
const tokens = computed(() =>
  readme.value ? Lexer.lex(readme.value.markdown) : []
)
const Document: FunctionalComponent = () => tokens.value.map(render)
</script>
<template>
  <section v-if="readme || error" class="sibling-space-y-3">
    <h3 class="font-medium">{{ t('pluginExtensions.readme') }}</h3>
    <ErrorBanner v-if="error" :message="error" @retry="load" />
    <div v-else class="sibling-space-y-3 break-words text-sm"><Document /></div>
  </section>
</template>
