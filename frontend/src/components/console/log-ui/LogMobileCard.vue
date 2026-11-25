<script setup lang="ts">
import { computed, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, routerKey } from 'vue-router'

import StatusChip from '@/components/common/StatusChip.vue'
import { getLocale } from '@/i18n'
import type { LogItem, LogType } from '@/types/console'
import { formatQuota, formatTime } from '@/utils/format'

import {
  logCallSubkind,
  logLinkedTask,
  type CallLogSubkind,
} from './logClassification'
import { logContentText, logTokenName } from './logContent'
import LogDetailTrigger from './LogDetailTrigger.vue'
import LogModelMismatch from './LogModelMismatch.vue'
import LogPerformanceCell from './LogPerformanceCell.vue'
import LogReasoningEffort from './LogReasoningEffort.vue'
import LogUsageCell from './LogUsageCell.vue'

const props = defineProps<{
  log: LogItem
  showAccount?: boolean
}>()

const emit = defineEmits<{
  'view-details': [log: LogItem]
}>()

const { t } = useI18n()
const hasRouter = Boolean(inject(routerKey, null))

const typeTone: Record<
  LogType,
  'accent' | 'success' | 'warning' | 'info' | 'danger'
> = {
  consume: 'accent',
  topup: 'success',
  refund: 'warning',
  manage: 'info',
  error: 'danger',
  system: 'info',
  login: 'info',
}

const typeLabelKey: Record<LogType, string> = {
  consume: 'logs.typeConsume',
  topup: 'logs.typeTopup',
  refund: 'logs.typeRefund',
  manage: 'logs.typeManage',
  error: 'logs.typeError',
  system: 'logs.typeSystem',
  login: 'logs.typeLogin',
}

const subkindLabelKey: Record<Exclude<CallLogSubkind, null>, string> = {
  violation_fee: 'logs.callSubtypeViolationFee',
  channel_test: 'logs.callSubtypeChannelTest',
  task_billing: 'logs.callSubtypeTaskBilling',
}

const subkindTone: Record<
  Exclude<CallLogSubkind, null>,
  'danger' | 'neutral' | 'info'
> = {
  violation_fee: 'danger',
  channel_test: 'neutral',
  task_billing: 'info',
}

const subkind = computed(() => logCallSubkind(props.log))
const linkedTask = computed(() => logLinkedTask(props.log))
</script>

<template>
  <article
    data-log-mobile-card
    :data-log-type="log.type"
    class="min-w-0 rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-4"
    :aria-label="t('logs.mobileCardLabel', { model: log.model })"
  >
    <header class="flex min-w-0 items-start justify-between gap-3">
      <div class="min-w-0">
        <div class="flex min-w-0 flex-wrap items-center gap-2">
          <h3
            class="max-w-full truncate font-mono text-base font-semibold leading-5 text-[var(--text-primary)]"
            :title="log.model"
          >
            {{ log.model }}
          </h3>
          <StatusChip :tone="typeTone[log.type]">
            {{ t(typeLabelKey[log.type]) }}
          </StatusChip>
          <StatusChip v-if="subkind" :tone="subkindTone[subkind]">
            {{ t(subkindLabelKey[subkind]) }}
          </StatusChip>
        </div>
        <LogModelMismatch :log="log" />
        <p class="mt-1 truncate text-xs text-[var(--text-tertiary)]">
          <template v-if="showAccount && log.username">
            {{ log.username }} ·
          </template>
          <template v-if="log.channel">{{ log.channel }} · </template>
          {{ formatTime(log.created) }}
        </p>
      </div>
      <p
        data-log-cost
        class="shrink-0 text-sm font-semibold tabular-nums"
        :class="
          ['topup', 'refund', 'manage', 'system'].includes(log.type)
            ? 'text-[var(--status-success-text)]'
            : 'text-[var(--text-primary)]'
        "
      >
        {{ formatQuota(log.quota.replace(/^-/, '')) }}
      </p>
    </header>

    <div
      class="mt-3 min-h-[58px] rounded-lg bg-[var(--surface-muted)] px-3 py-2.5"
    >
      <LogPerformanceCell :log="log" :interactive="false" />
    </div>

    <dl class="mt-3 grid min-w-0 grid-cols-2 gap-x-4 gap-y-3 text-xs">
      <div class="min-w-0">
        <dt class="text-[var(--text-tertiary)]">{{ t('logs.colToken') }}</dt>
        <dd
          class="mt-0.5 truncate font-mono text-[var(--text-secondary)]"
          :title="logTokenName(log, getLocale())"
        >
          {{ logTokenName(log, getLocale()) }}
        </dd>
      </div>
      <div class="min-w-0">
        <dt class="text-[var(--text-tertiary)]">
          {{ t('logs.colReasoning') }}
        </dt>
        <dd class="mt-1">
          <LogReasoningEffort :log="log" mobile />
        </dd>
      </div>
      <div
        class="col-span-2 min-w-0 border-t border-[var(--border-subtle)] pt-3"
      >
        <dt class="text-[var(--text-tertiary)]">{{ t('logs.colUsage') }}</dt>
        <dd class="mt-1">
          <LogUsageCell :log="log" mobile />
        </dd>
      </div>
      <div
        v-if="linkedTask && hasRouter"
        class="col-span-2 min-w-0 border-t border-[var(--border-subtle)] pt-3"
      >
        <RouterLink
          :to="{ name: linkedTask.routeName, query: linkedTask.query }"
          class="inline-flex min-h-8 items-center text-xs font-medium text-[var(--accent-text)] underline-offset-2 hover:underline"
        >
          {{ t('logs.openLinkedTask') }} · {{ linkedTask.taskId }}
        </RouterLink>
      </div>
      <div
        class="col-span-2 min-w-0 border-t border-[var(--border-subtle)] pt-3"
      >
        <dt class="sr-only">{{ t('logs.colContent') }}</dt>
        <dd>
          <LogDetailTrigger
            :content="logContentText(log, getLocale())"
            :label="t('logs.viewLogDetails')"
            @activate="emit('view-details', log)"
          />
        </dd>
      </div>
    </dl>
  </article>
</template>
