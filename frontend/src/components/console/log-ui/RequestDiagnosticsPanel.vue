<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getRequestDiagnostic,
  type RequestDiagnostic,
  type RequestDiagnosticFilter,
} from '@/api/requestDiagnostics'
import { ApiError } from '@/api/types'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { useLatestRequest } from '@/composables/useLatestRequest'
import { requestDiagnosticsMessages } from '@/i18n/requestDiagnostics'
import RequestDiagnosticsTimeline from './RequestDiagnosticsTimeline.vue'

const props = withDefaults(
  defineProps<{ requestId?: string; admin?: boolean }>(),
  { requestId: '', admin: false }
)
const emit = defineEmits<{ related: [filter: RequestDiagnosticFilter] }>()
const { t } = useI18n({
  useScope: 'local',
  messages: requestDiagnosticsMessages,
})
const diagnostic = ref<RequestDiagnostic | null>(null)
const loading = ref(false)
const error = ref('')
const unavailable = ref(false)
const request = useLatestRequest()
async function load() {
  request.cancel()
  diagnostic.value = null
  error.value = ''
  unavailable.value = !props.requestId
  loading.value = false
  if (!props.requestId) return
  loading.value = true
  const result = await request.run((signal) =>
    getRequestDiagnostic(props.requestId, props.admin, signal)
  )
  if (result.stale) return
  loading.value = false
  if (!result.ok) {
    unavailable.value =
      result.error instanceof ApiError && result.error.status === 404
    if (!unavailable.value)
      error.value =
        result.error instanceof Error
          ? result.error.message
          : String(result.error)
    return
  }
  diagnostic.value = result.value
}
watch(() => [props.requestId, props.admin], load, { immediate: true })
</script>

<template>
  <section class="mt-5 space-y-3 border-t border-[var(--border-subtle)] pt-5">
    <h3 class="font-medium">{{ t('title') }}</h3>
    <p class="text-xs text-[var(--text-secondary)]">{{ t('hint') }}</p>
    <p v-if="loading" role="status" class="text-sm">{{ t('loading') }}</p>
    <p
      v-else-if="unavailable"
      data-diagnostics-unavailable
      class="text-sm text-[var(--text-secondary)]"
    >
      {{ t('unavailable') }}
    </p>
    <p
      v-else-if="error"
      role="alert"
      class="break-all text-sm text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
    <RequestDiagnosticsTimeline
      v-if="diagnostic"
      :diagnostic="diagnostic"
      :admin="admin"
      @related="emit('related', $event)"
    />
    <ConsoleButton
      v-if="requestId && (unavailable || error)"
      variant="secondary"
      size="sm"
      @click="load"
      >{{ t('retry') }}</ConsoleButton
    >
  </section>
</template>
