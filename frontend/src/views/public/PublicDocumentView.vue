<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { ExternalLink } from 'lucide-vue-next'
import { publicCatalogApi, type PublicDocument } from '@/api/publicCatalog'
import PublicPage from '@/components/public/PublicPage.vue'
import {
  publicDocumentMarkup,
  publicDocumentUrl,
} from '@/components/public/documentMarkup'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import { useTheme } from '@/composables/useTheme'
import { publicPageMessages } from '@/i18n/publicPages'

const props = defineProps<{ kind: PublicDocument }>()
const { t } = useI18n({ useScope: 'local', messages: publicPageMessages })
const { resolvedTheme } = useTheme()
const content = ref('')
const loading = ref(false)
const error = ref('')
let controller: AbortController | undefined
const documentUrl = computed(() => publicDocumentUrl(content.value))
const markup = computed(() => {
  const scheme = resolvedTheme.value
  const styles = getComputedStyle(document.documentElement)
  return publicDocumentMarkup(content.value, {
    scheme,
    background: styles.getPropertyValue('--page-background').trim(),
    foreground: styles.getPropertyValue('--text-primary').trim(),
    accent: styles.getPropertyValue('--accent-text').trim(),
  })
})

async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  content.value = ''
  error.value = ''
  try {
    content.value = await publicCatalogApi.document(props.kind, request.signal)
  } catch (cause) {
    if (!request.signal.aborted)
      error.value =
        cause instanceof Error ? cause.message : t('common.loadFailed')
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}
watch(() => props.kind, load, { immediate: true })
onScopeDispose(() => controller?.abort())
</script>

<template>
  <PublicPage :title="t(`publicPage.${kind}`)">
    <template #actions
      ><a
        v-if="documentUrl"
        :href="documentUrl"
        target="_blank"
        rel="noopener noreferrer"
        class="inline-flex min-h-11 items-center gap-2 text-sm text-[var(--accent-text)]"
        ><ExternalLink :size="16" />{{ t('publicPage.openDocument') }}</a
      ></template
    >
    <div
      v-if="loading"
      role="status"
      :aria-label="t('publicPage.loading')"
      class="space-y-4"
    >
      <SkeletonBlock class="h-10 w-48" /><SkeletonBlock class="h-64 w-full" />
    </div>
    <ErrorBanner v-else-if="error" :message="error" @retry="load" />
    <EmptyState
      v-else-if="!content.trim()"
      :title="t('publicPage.empty')"
      :hint="t('publicPage.documentMissing')"
    />
    <iframe
      v-else-if="documentUrl"
      :src="documentUrl"
      :title="t(`publicPage.${kind}`)"
      sandbox="allow-popups allow-popups-to-escape-sandbox"
      referrerpolicy="no-referrer"
      class="h-[72vh] min-h-96 w-full border-0"
    />
    <iframe
      v-else
      :srcdoc="markup"
      :title="t(`publicPage.${kind}`)"
      sandbox="allow-popups allow-popups-to-escape-sandbox"
      referrerpolicy="no-referrer"
      class="h-[72vh] min-h-96 w-full border-0"
    />
  </PublicPage>
</template>
