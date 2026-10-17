<script setup lang="ts">
import { ref, watch } from 'vue'
import { Pencil, RefreshCw } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import { api } from '@/api/console'
import { parseToken } from '@/api/liveContracts'
import { useLatestRequest } from '@/composables/useLatestRequest'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import KeyFormModal from '@/components/console/keys/KeyFormModal.vue'
import type { TokenSummary } from '@/types/console'
const props = defineProps<{ tokenId: number }>()
const emit = defineEmits<{ updated: [] }>()
const { t } = useI18n()
const request = useLatestRequest()
const token = ref<TokenSummary | null>(null)
const editing = ref(false)
const loading = ref(false)
const error = ref('')
async function load() {
  loading.value = true
  error.value = ''
  token.value = null
  const endpoint = `/api/token/${props.tokenId}`
  const result = await request.run(async (signal) =>
    parseToken(
      await api.get<unknown>(endpoint, undefined, { signal }),
      endpoint
    )
  )
  if (result.stale) return
  loading.value = false
  if (result.ok) token.value = result.value
  else error.value = String(result.error)
}
function tokenSaved(): void {
  emit('updated')
  void load()
}
watch(
  () => props.tokenId,
  () => {
    editing.value = false
    void load()
  },
  { immediate: true }
)
</script>
<template>
  <section class="min-w-0 border-t border-[var(--border-subtle)] py-4">
    <div class="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h3 class="text-sm font-semibold">
          {{ t('gateway.conversionPolicy') }}
        </h3>
        <p v-if="token" class="mt-1 text-sm text-[var(--text-secondary)]">
          {{ t(`gateway.conversion.${token.conversion_policy ?? 'safe'}`) }}
        </p>
      </div>
      <ConsoleButton
        v-if="token"
        variant="secondary"
        size="sm"
        @click="editing = true"
        ><Pencil class="h-4 w-4" />{{ t('common.edit') }}</ConsoleButton
      >
      <ConsoleButton
        v-else
        variant="ghost"
        size="sm"
        :disabled="loading"
        :title="t('gateway.reload')"
        :aria-label="t('gateway.reload')"
        @click="load"
        ><RefreshCw class="h-4 w-4"
      /></ConsoleButton>
    </div>
    <p
      v-if="error"
      role="alert"
      class="mt-2 break-words text-sm text-[var(--status-danger-text)]"
    >
      {{ error }}
    </p>
    <KeyFormModal
      :open="editing"
      :editing="token"
      :models="token?.model_limits ?? []"
      @close="editing = false"
      @saved="tokenSaved"
    />
  </section>
</template>
