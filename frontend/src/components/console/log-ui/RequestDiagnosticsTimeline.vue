<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type {
  RequestDiagnostic,
  RequestDiagnosticFilter,
} from '@/api/requestDiagnostics'
import StatusChip from '@/components/common/StatusChip.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { requestDiagnosticsMessages } from '@/i18n/requestDiagnostics'

withDefaults(
  defineProps<{ diagnostic: RequestDiagnostic; admin?: boolean }>(),
  { admin: false }
)
const emit = defineEmits<{ related: [filter: RequestDiagnosticFilter] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: requestDiagnosticsMessages,
})
const time = (value?: number) =>
  value
    ? new Date(value).toLocaleString(locale.value, { hour12: false })
    : t('unknown')
</script>

<template>
  <section data-request-diagnostics class="sibling-space-y-4 text-left">
    <dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2 text-xs">
      <dt>{{ t('request') }}</dt>
      <dd class="break-all font-mono">{{ diagnostic.request_id }}</dd>
      <dt>{{ t('token') }}</dt>
      <dd>
        {{
          diagnostic.token_id > 0 ? diagnostic.token_id : t('transientToken')
        }}
      </dd>
      <dt>{{ t('model') }}</dt>
      <dd class="break-all">{{ diagnostic.model_name }}</dd>
      <template v-if="diagnostic.trace_id"
        ><dt>{{ t('trace') }}</dt>
        <dd class="break-all font-mono">{{ diagnostic.trace_id }}</dd></template
      >
      <template v-if="diagnostic.thread_id"
        ><dt>{{ t('thread') }}</dt>
        <dd class="break-all font-mono">
          {{ diagnostic.thread_id }}
        </dd></template
      >
      <dt>{{ t('wait') }}</dt>
      <dd>{{ diagnostic.wait_ms }} ms</dd>
      <dt>{{ t('userQueue') }}</dt>
      <dd>{{ diagnostic.user_queue_ms }} ms</dd>
      <template v-if="diagnostic.final_error"
        ><dt>{{ t('finalError') }}</dt>
        <dd class="break-all text-[var(--status-danger-text)]">
          {{ diagnostic.final_error }}
        </dd></template
      >
    </dl>
    <div
      v-if="
        diagnostic.token_id > 0 && (diagnostic.trace_id || diagnostic.thread_id)
      "
      class="flex flex-wrap gap-2"
    >
      <ConsoleButton
        v-if="diagnostic.trace_id"
        variant="secondary"
        size="sm"
        @click="
          emit('related', {
            token_id: diagnostic.token_id,
            trace_id: diagnostic.trace_id,
          })
        "
        >{{ t('relatedTrace') }}</ConsoleButton
      >
      <ConsoleButton
        v-if="diagnostic.thread_id"
        variant="secondary"
        size="sm"
        @click="
          emit('related', {
            token_id: diagnostic.token_id,
            thread_id: diagnostic.thread_id,
          })
        "
        >{{ t('relatedThread') }}</ConsoleButton
      >
    </div>
    <p
      v-if="diagnostic.token_id === 0"
      class="text-xs text-[var(--text-secondary)]"
    >
      {{ t('transientHint') }}
    </p>
    <p
      v-if="!diagnostic.attempts.length"
      class="text-sm text-[var(--text-secondary)]"
    >
      {{ t('noAttempts') }}
    </p>
    <ol
      v-else
      class="sibling-space-y-3 border-l-2 border-[var(--border-subtle)] pl-3"
    >
      <li
        v-for="attempt in diagnostic.attempts"
        :key="attempt.attempt_id"
        class="min-w-0 rounded-lg border border-[var(--border-subtle)] p-3"
        data-diagnostic-attempt
      >
        <div class="mb-3 flex flex-wrap items-center justify-between gap-2">
          <h4 class="text-sm font-medium">
            {{ t('attempt', { sequence: attempt.sequence }) }}
          </h4>
          <StatusChip :tone="attempt.success ? 'success' : 'danger'">{{
            t(attempt.success ? 'success' : 'failed')
          }}</StatusChip>
        </div>
        <p class="mb-3 break-all font-mono text-xs text-[var(--text-tertiary)]">
          {{ attempt.attempt_id }}
        </p>
        <p
          v-if="attempt.error_class || attempt.error_http_status"
          class="mb-3 break-all text-xs text-[var(--status-danger-text)]"
        >
          {{ attempt.error_class }} {{ attempt.error_http_status || '' }}
        </p>
        <dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-2 text-xs">
          <dt>{{ t('channelQueue') }}</dt>
          <dd>{{ attempt.queue_ms }} ms</dd>
          <dt>{{ t('latency') }}</dt>
          <dd data-upstream-duration>
            {{
              attempt.upstream_latency_ms == null
                ? t('unknown')
                : `${attempt.upstream_latency_ms} ms`
            }}
          </dd>
          <dt>{{ t('attemptLatency') }}</dt>
          <dd>{{ attempt.latency_ms }} ms</dd>
          <dt>{{ t('started') }}</dt>
          <dd>{{ time(attempt.send_started_at) }}</dd>
          <dt>{{ t('firstOutput') }}</dt>
          <dd>
            {{ time(attempt.first_output_at)
            }}<span v-if="attempt.ttft_ms != null">
              · {{ attempt.ttft_ms }} ms</span
            >
          </dd>
          <dt>{{ t('finished') }}</dt>
          <dd>{{ time(attempt.finished_at) }}</dd>
          <dt>{{ t('origin') }}</dt>
          <dd>{{ attempt.origin || t('unknown') }}</dd>
          <dt>{{ t('acceptance') }}</dt>
          <dd>{{ attempt.acceptance || t('unknown') }}</dd>
          <dt>{{ t('retryPermission') }}</dt>
          <dd>{{ attempt.retry_permission || t('unknown') }}</dd>
          <dt>{{ t('committed') }}</dt>
          <dd>{{ t(attempt.response_committed ? 'yes' : 'no') }}</dd>
          <template v-if="attempt.termination"
            ><dt>{{ t('termination') }}</dt>
            <dd>{{ attempt.termination }}</dd></template
          >
          <template v-if="admin && attempt.channel_id != null"
            ><dt>{{ t('channel') }}</dt>
            <dd>{{ attempt.channel_id }}</dd></template
          >
          <template v-if="admin && attempt.credential_id"
            ><dt>{{ t('credential') }}</dt>
            <dd class="break-all font-mono">
              {{ attempt.credential_id }}
            </dd></template
          >
        </dl>
      </li>
    </ol>
  </section>
</template>
