<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, useId } from 'vue'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/client'
import { invalidResponse, isRecord } from '@/api/contracts'
const model = defineModel<string>({ required: true })
const { t } = useI18n()
const id = useId()
const suggestions = ref<string[]>([])
const error = ref('')
let controller: AbortController | undefined
async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  error.value = ''
  try {
    const endpoint = '/api/user/self/groups'
    const data = await api.get<unknown>(endpoint, undefined, {
      signal: request.signal,
    })
    if (request.signal.aborted) return
    if (!isRecord(data)) invalidResponse(endpoint)
    suggestions.value = Object.keys(data).filter((group) => group !== 'auto')
  } catch (cause) {
    if (!request.signal.aborted)
      error.value = cause instanceof Error ? cause.message : String(cause)
  }
}
onMounted(load)
onBeforeUnmount(() => {
  controller?.abort()
})
</script>
<template>
  <div class="min-w-0 w-full sm:w-48">
    <input
      v-model="model"
      :list="id"
      :aria-label="t('logs.groupLabel')"
      :placeholder="t('logs.groupPlaceholder')"
      autocomplete="off"
      name="log-group-filter"
      maxlength="64"
      class="focus-ring h-10 w-full rounded-lg border border-[var(--border-default)] bg-[var(--surface-solid)] px-3 text-sm"
    />
    <datalist :id="id">
      <option v-for="group in suggestions" :key="group" :value="group" />
    </datalist>
    <p
      v-if="error"
      role="alert"
      class="mt-1 text-xs text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
  </div>
</template>
