<script setup lang="ts">
import { computed, inject } from 'vue'
import { Copy } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { RouterLink, routerKey } from 'vue-router'

import StatusChip from '@/components/common/StatusChip.vue'
import { useToast } from '@/composables/useToast'
import { getLocale } from '@/i18n'
import type { LogItem } from '@/types/console'
import { formatQuota, formatTime } from '@/utils/format'
import { logEvidence } from '@/utils/logModel'

import {
  classifyQuotaLog,
  logLinkedTask,
  quotaLogDescription,
  type QuotaEntrySummary,
} from './logClassification'
import { logContentText, logTokenName } from './logContent'

const props = withDefaults(
  defineProps<{
    log: LogItem
    mode?: 'call' | 'quota'
  }>(),
  { mode: 'call' }
)
const { t } = useI18n()
const toast = useToast()
const hasRouter = Boolean(inject(routerKey, null))

const details = computed(() => logEvidence(props.log))
const record = computed(() => {
  const data = details.value.request_plugins
  return Array.isArray(data) ? data : []
})
const rawDetails = computed(() => {
  if (!record.value.length) return details.value
  const rest = { ...details.value }
  delete rest.request_plugins
  return rest
})
const linkedTask = computed(() => logLinkedTask(props.log))
const quotaSummary = computed(() =>
  props.mode === 'quota' ? classifyQuotaLog(props.log) : null
)
const descriptionText = computed(() => {
  if (props.mode === 'quota') {
    return quotaLogDescription(props.log, getLocale(), t)
  }
  return logContentText(props.log, getLocale())
})

const amountToneClass: Record<QuotaEntrySummary['amountTone'], string> = {
  success: 'text-[var(--status-success-text)]',
  danger: 'text-[var(--status-danger-text)]',
  warning: 'text-[var(--status-warning-text)]',
  neutral: 'text-[var(--text-secondary)]',
}

async function copyPayload() {
  try {
    await navigator.clipboard.writeText(JSON.stringify(details.value, null, 2))
    toast.success(t('common.copied'))
  } catch {
    toast.error(t('common.copyFailed'))
  }
}
</script>

<template>
  <div class="sibling-space-y-4 text-left">
    <dl class="grid gap-2.5 sm:grid-cols-2">
      <div
        v-if="quotaSummary"
        class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3"
      >
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('quotaLogs.colKind') }}
        </dt>
        <dd class="mt-1">
          <StatusChip :tone="quotaSummary.tone">
            {{ t(quotaSummary.kindLabelKey) }}
          </StatusChip>
        </dd>
      </div>
      <div
        v-if="quotaSummary"
        data-quota-detail-amount
        class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3"
      >
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('quotaLogs.colAmount') }}
        </dt>
        <dd
          class="mt-1 font-mono text-sm font-semibold tabular-nums"
          :class="amountToneClass[quotaSummary.amountTone]"
        >
          {{ quotaSummary.amountText ?? '—' }}
        </dd>
      </div>

      <template v-else>
        <div
          v-if="log.model"
          class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3"
        >
          <dt class="text-xs text-[var(--text-tertiary)]">
            {{ t('billing.clientModel') }}
          </dt>
          <dd
            class="mt-1 break-all font-mono text-sm text-[var(--text-primary)]"
          >
            {{ log.model }}
          </dd>
        </div>
        <div
          class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3"
        >
          <dt class="text-xs text-[var(--text-tertiary)]">
            {{ t('billing.finalCharge') }}
          </dt>
          <dd
            class="mt-1 font-mono text-sm font-semibold tabular-nums text-[var(--text-primary)]"
          >
            {{ formatQuota(log.quota) }}
          </dd>
        </div>
      </template>

      <div
        v-if="log.created"
        class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3"
      >
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('logs.colTime') }}
        </dt>
        <dd
          class="mt-1 font-mono text-xs tabular-nums text-[var(--text-primary)]"
        >
          {{ formatTime(log.created) }}
        </dd>
      </div>
      <div
        v-if="log.username"
        class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3"
      >
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('logs.colAccount') }}
        </dt>
        <dd class="mt-1 break-words text-sm text-[var(--text-primary)]">
          {{ log.username }}
        </dd>
      </div>
      <div
        v-if="log.channel"
        class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3"
      >
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('logs.colChannel') }}
        </dt>
        <dd class="mt-1 break-words text-sm text-[var(--text-primary)]">
          {{ log.channel }}
        </dd>
      </div>
      <div
        v-if="log.token_name"
        class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3"
      >
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('logs.colToken') }}
        </dt>
        <dd class="mt-1 break-all font-mono text-xs text-[var(--text-primary)]">
          {{ logTokenName(log, getLocale()) }}
        </dd>
      </div>
      <div
        v-if="log.request_id"
        class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3 sm:col-span-2"
      >
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('logs.requestIdLabel') }}
        </dt>
        <dd class="mt-1 break-all font-mono text-xs text-[var(--text-primary)]">
          {{ log.request_id }}
        </dd>
      </div>
      <div
        v-if="log.upstream_request_id"
        class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3 sm:col-span-2"
      >
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('logs.upstreamRequestIdLabel') }}
        </dt>
        <dd class="mt-1 break-all font-mono text-xs text-[var(--text-primary)]">
          {{ log.upstream_request_id }}
        </dd>
      </div>
      <div
        v-if="linkedTask"
        class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3 sm:col-span-2"
      >
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('relayLogs.colTask') }}
        </dt>
        <dd class="mt-1 flex flex-wrap items-center gap-2">
          <span
            class="break-all font-mono text-xs text-[var(--text-primary)]"
            >{{ linkedTask.taskId }}</span
          >
          <RouterLink
            v-if="hasRouter"
            :to="{ name: linkedTask.routeName, query: linkedTask.query }"
            class="text-xs font-medium text-[var(--accent-text)] underline-offset-2 hover:underline"
          >
            {{ t('logs.openLinkedTask') }}
          </RouterLink>
        </dd>
      </div>
      <div
        v-if="descriptionText"
        class="rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3 sm:col-span-2"
      >
        <dt class="text-xs text-[var(--text-tertiary)]">
          {{ t('logs.summaryLabel') }}
        </dt>
        <dd
          class="mt-1 whitespace-pre-wrap break-words text-sm leading-5 text-[var(--text-primary)]"
        >
          {{ descriptionText }}
        </dd>
      </div>
    </dl>

    <section v-if="record.length" class="sibling-space-y-2">
      <h3 class="text-xs font-semibold text-[var(--text-secondary)]">
        {{ t('pluginExtensions.record') }}
      </h3>
      <pre
        data-log-request-plugins
        class="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3 font-mono text-xs text-[var(--text-primary)]"
        >{{ JSON.stringify(record, null, 2) }}</pre>
    </section>

    <section class="sibling-space-y-2">
      <div class="flex items-center justify-between gap-2">
        <h3 class="text-xs font-semibold text-[var(--text-secondary)]">
          {{ t('logs.rawEvidence') }}
        </h3>
        <button
          type="button"
          class="inline-flex items-center gap-1 rounded-md px-2 py-1 text-xs font-medium text-[var(--text-tertiary)] transition-colors hover:bg-[var(--state-hover-layer)] hover:text-[var(--text-primary)] focus-ring"
          @click="copyPayload"
        >
          <Copy :size="13" aria-hidden="true" />
          {{ t('logs.copyJson') }}
        </button>
      </div>
      <pre
        data-log-raw-evidence
        class="max-h-[45vh] overflow-auto rounded-lg border border-[var(--outline-variant)] bg-[var(--wallet-tile-surface)] p-3.5 font-mono text-xs text-[var(--text-primary)]"
        >{{ JSON.stringify(rawDetails, null, 2) }}</pre>
    </section>
  </div>
</template>
