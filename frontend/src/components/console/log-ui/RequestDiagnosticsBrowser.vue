<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  listRequestDiagnostics,
  type RequestDiagnostic,
  type RequestDiagnosticFilter,
} from '@/api/requestDiagnostics'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { requestDiagnosticsMessages } from '@/i18n/requestDiagnostics'
import RequestDiagnosticsTimeline from './RequestDiagnosticsTimeline.vue'

const props = withDefaults(
  defineProps<{
    canReadAll?: boolean
    initialFilter?: RequestDiagnosticFilter
  }>(),
  { canReadAll: false, initialFilter: undefined }
)
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n({
  useScope: 'local',
  messages: requestDiagnosticsMessages,
})
const tokenID = ref(
  props.initialFilter ? String(props.initialFilter.token_id) : ''
)
const traceID = ref(props.initialFilter?.trace_id ?? '')
const threadID = ref(props.initialFilter?.thread_id ?? '')
const requestID = ref(props.initialFilter?.request_id ?? '')
const scope = ref<'self' | 'all'>('self')
const rows = ref<RequestDiagnostic[]>([])
const selected = ref<RequestDiagnostic | null>(null)
const page = ref(1)
const pageSize = ref(10)
const total = ref(0)
const loading = ref(false)
const searched = ref(false)
const error = ref('')
const request = useLatestRequest()

async function load() {
  request.cancel()
  rows.value = []
  selected.value = null
  total.value = 0
  error.value = ''
  loading.value = false
  const token = Number(tokenID.value)
  if (!Number.isSafeInteger(token) || token < 1) {
    error.value = t('tokenRequired')
    return
  }
  const filter: RequestDiagnosticFilter = { token_id: token }
  if (traceID.value.trim()) filter.trace_id = traceID.value.trim()
  if (threadID.value.trim()) filter.thread_id = threadID.value.trim()
  if (requestID.value.trim()) filter.request_id = requestID.value.trim()
  loading.value = true
  searched.value = true
  const result = await request.run((signal) =>
    listRequestDiagnostics(
      filter,
      page.value,
      pageSize.value,
      scope.value === 'all' && props.canReadAll,
      signal
    )
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    error.value =
      result.error instanceof Error
        ? result.error.message
        : String(result.error)
    return
  }
  rows.value = result.value.items
  total.value = result.value.total
}
function search() {
  if (page.value !== 1) page.value = 1
  else void load()
}
function related(filter: RequestDiagnosticFilter) {
  tokenID.value = String(filter.token_id)
  traceID.value = filter.trace_id ?? ''
  threadID.value = filter.thread_id ?? ''
  requestID.value = filter.request_id ?? ''
  search()
}
watch([page, pageSize], load)
watch(scope, () => {
  request.cancel()
  rows.value = []
  selected.value = null
  if (tokenID.value) search()
})
watch(
  () => props.canReadAll,
  (allowed) => {
    if (!allowed) scope.value = 'self'
  }
)
if (props.initialFilter) void load()
</script>

<template>
  <ConsoleModal
    :open="true"
    :title="t('title')"
    :subtitle="t('hint')"
    size="xl"
    @close="emit('close')"
  >
    <div class="sibling-space-y-4">
      <form class="grid gap-3 sm:grid-cols-2" @submit.prevent="search">
        <label class="text-sm"
          >{{ t('token')
          }}<input
            v-model="tokenID"
            :aria-label="t('token')"
            type="number"
            min="1"
            step="1"
            required
            class="console-input mt-1 w-full"
        /></label>
        <label v-if="canReadAll" class="text-sm"
          >{{ t('scope')
          }}<select
            v-model="scope"
            :aria-label="t('scope')"
            class="console-input mt-1 w-full"
          >
            <option value="self">{{ t('self') }}</option>
            <option value="all">{{ t('all') }}</option>
          </select></label
        >
        <label class="text-sm"
          >{{ t('trace')
          }}<input
            v-model="traceID"
            :aria-label="t('trace')"
            maxlength="128"
            class="console-input mt-1 w-full font-mono"
        /></label>
        <label class="text-sm"
          >{{ t('thread')
          }}<input
            v-model="threadID"
            :aria-label="t('thread')"
            maxlength="128"
            class="console-input mt-1 w-full font-mono"
        /></label>
        <label class="text-sm"
          >{{ t('request')
          }}<input
            v-model="requestID"
            :aria-label="t('request')"
            maxlength="128"
            class="console-input mt-1 w-full font-mono"
        /></label>
        <div class="flex items-end">
          <ConsoleButton type="submit" :loading="loading">{{
            t('search')
          }}</ConsoleButton>
        </div>
      </form>
      <p
        v-if="error"
        role="alert"
        class="break-all text-sm text-[var(--status-danger-text)]"
      >
        {{ error }}
      </p>
      <p v-if="loading" role="status" class="text-sm">{{ t('loading') }}</p>
      <p
        v-else-if="searched && !rows.length && !error"
        class="text-sm text-[var(--text-secondary)]"
      >
        {{ t('empty') }}
      </p>
      <ul
        v-if="rows.length"
        class="sibling-divide-y sibling-divide-[var(--border-subtle)] rounded-lg border border-[var(--border-subtle)]"
      >
        <li
          v-for="row in rows"
          :key="row.request_id"
          class="flex min-w-0 flex-wrap items-center justify-between gap-3 p-3"
        >
          <div class="min-w-0 flex-1">
            <p class="break-all font-mono text-xs">{{ row.request_id }}</p>
            <p class="mt-1 break-all text-xs text-[var(--text-secondary)]">
              {{ row.model_name }}
            </p>
          </div>
          <ConsoleButton
            variant="secondary"
            size="sm"
            :aria-expanded="selected?.request_id === row.request_id"
            @click="
              selected = selected?.request_id === row.request_id ? null : row
            "
            >{{ t('details') }}</ConsoleButton
          >
        </li>
      </ul>
      <TablePagination
        v-if="searched && !error"
        v-model:page="page"
        v-model:page-size="pageSize"
        :total="total"
      />
      <RequestDiagnosticsTimeline
        v-if="selected"
        :diagnostic="selected"
        :admin="scope === 'all' && canReadAll"
        @related="related"
      />
    </div>
    <template #footer
      ><ConsoleButton variant="secondary" @click="emit('close')">{{
        t('close')
      }}</ConsoleButton></template
    >
  </ConsoleModal>
</template>
