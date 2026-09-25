<script setup lang="ts">
import { computed } from 'vue'
import { AlertTriangle } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import type { LogItem } from '@/types/console'
import { logModelObservation } from '@/utils/logModel'
const props = defineProps<{ log: LogItem }>()
const { t } = useI18n()
const observation = computed(() => logModelObservation(props.log))
</script>
<template>
  <span
    v-if="observation.mismatch"
    data-log-model-mismatch
    class="mt-1 inline-flex max-w-full items-center gap-1 text-xs text-[var(--status-warning-text)]"
    :title="
      t('billing.responseModelMismatchDetail', { model: observation.response })
    "
  >
    <AlertTriangle :size="13" class="shrink-0" aria-hidden="true" />{{
      t('billing.responseModelMismatch')
    }}
  </span>
</template>
