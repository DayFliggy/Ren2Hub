<script setup lang="ts">
import { computed, inject } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink, routerKey } from 'vue-router'
import { logEvidence } from '@/utils/logModel'
import type { LogItem } from '@/types/console'
import { formatQuota } from '@/utils/format'
import { logLinkedTask } from './logClassification'

const props = defineProps<{ log: LogItem }>()
const { t } = useI18n()
const hasRouter = Boolean(inject(routerKey, null))

const record = computed(() => {
  const data = logEvidence(props.log).request_plugins
  return Array.isArray(data) ? data : []
})
const details = computed(() => logEvidence(props.log))
const linkedTask = computed(() => logLinkedTask(props.log))
</script>

<template>
  <div class="space-y-4 text-left">
    <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
      <template v-if="log.model">
        <dt class="text-[var(--text-tertiary)]">
          {{ t('billing.clientModel') }}
        </dt>
        <dd class="break-all font-mono">{{ log.model }}</dd>
      </template>
      <dt class="text-[var(--text-tertiary)]">
        {{ t('billing.finalCharge') }}
      </dt>
      <dd class="tabular-nums">{{ formatQuota(log.quota) }}</dd>
      <template v-if="log.request_id">
        <dt class="text-[var(--text-tertiary)]">
          {{ t('logs.requestIdLabel') }}
        </dt>
        <dd class="break-all font-mono text-xs">{{ log.request_id }}</dd>
      </template>
      <template v-if="log.upstream_request_id">
        <dt class="text-[var(--text-tertiary)]">
          {{ t('logs.upstreamRequestIdLabel') }}
        </dt>
        <dd class="break-all font-mono text-xs">
          {{ log.upstream_request_id }}
        </dd>
      </template>
      <template v-if="linkedTask">
        <dt class="text-[var(--text-tertiary)]">
          {{ t('relayLogs.colTask') }}
        </dt>
        <dd class="flex flex-wrap items-center gap-2">
          <span class="break-all font-mono text-xs">{{
            linkedTask.taskId
          }}</span>
          <RouterLink
            v-if="hasRouter"
            :to="{ name: linkedTask.routeName, query: linkedTask.query }"
            class="text-xs font-medium text-[var(--accent-text)] underline-offset-2 hover:underline"
          >
            {{ t('logs.openLinkedTask') }}
          </RouterLink>
        </dd>
      </template>
    </dl>
    <section v-if="record.length" class="sibling-space-y-2">
      <h3 class="font-medium">{{ t('pluginExtensions.record') }}</h3>
      <pre
        class="max-h-80 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-[var(--surface-muted)] p-3 text-xs"
        >{{ JSON.stringify(record, null, 2) }}</pre>
    </section>
    <pre
      class="max-h-[60vh] overflow-auto rounded-lg bg-[var(--surface-muted)] p-4 text-xs"
      >{{ JSON.stringify(details, null, 2) }}</pre>
  </div>
</template>
