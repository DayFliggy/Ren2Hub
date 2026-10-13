<script setup lang="ts">
// Upstream components/plugin-integrity-check.tsx.
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  Info,
  LoaderCircle,
} from 'lucide-vue-next'

import CopyButton from './CopyButton.vue'
import PluginAlert from './PluginAlert.vue'

const props = withDefaults(
  defineProps<{
    expected?: string
    digest?: string | null
    loading: boolean
    sourceAvailable: boolean
  }>(),
  { expected: '', digest: null }
)
const { t } = useI18n()
const state = computed(() => {
  if (!props.expected)
    return {
      status: 'missing' as const,
      label: t('taskPlugins.noIntegrityHash'),
      desc: t('taskPlugins.integrityMissingDesc'),
    }
  if (props.loading)
    return {
      status: 'pending' as const,
      label: t('taskPlugins.checkingIntegrity'),
      desc: t('taskPlugins.integrityVerifiedDesc'),
    }
  if (props.digest) {
    const verified = props.digest.toLowerCase() === props.expected.toLowerCase()
    return verified
      ? {
          status: 'verified' as const,
          label: t('taskPlugins.integrityVerified'),
          desc: t('taskPlugins.integrityVerifiedDesc'),
        }
      : {
          status: 'failed' as const,
          label: t('taskPlugins.integrityFailed'),
          desc: t('taskPlugins.integrityFailedDesc'),
        }
  }
  return {
    status: 'unavailable' as const,
    label: props.sourceAvailable
      ? t('taskPlugins.integrityUnavailableEnv')
      : t('taskPlugins.integrityNotVerified'),
    desc: t('taskPlugins.integrityUnavailableDesc'),
  }
})
</script>

<template>
  <section
    class="sibling-space-y-1"
    :aria-label="t('taskPlugins.fileVerification')"
  >
    <PluginAlert
      :tone="state.status === 'failed' ? 'danger' : 'neutral'"
      :role="state.status === 'failed' ? 'alert' : 'status'"
      :title="state.label"
    >
      <template #icon>
        <LoaderCircle
          v-if="state.status === 'pending'"
          :size="16"
          class="animate-spin"
        />
        <CheckCircle2
          v-else-if="state.status === 'verified'"
          :size="16"
          class="text-[var(--status-success)]"
        />
        <AlertTriangle v-else-if="state.status === 'failed'" :size="16" />
        <Info v-else :size="16" />
      </template>
      {{ state.desc }}
    </PluginAlert>
    <details v-if="expected" class="group">
      <summary
        class="inline-flex cursor-pointer list-none items-center gap-1 px-1 py-1 text-xs text-[var(--text-secondary)] focus-ring"
      >
        {{ t('taskPlugins.integrityHash') }}
        <ChevronDown
          :size="12"
          class="transition-transform group-open:rotate-180"
          aria-hidden="true"
        />
      </summary>
      <div
        class="mt-2 flex min-w-0 items-start gap-2 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-muted)] p-3"
      >
        <code class="min-w-0 flex-1 break-all text-xs leading-relaxed">{{
          expected
        }}</code>
        <CopyButton :value="expected" :label="t('taskPlugins.integrityHash')" />
      </div>
    </details>
  </section>
</template>
