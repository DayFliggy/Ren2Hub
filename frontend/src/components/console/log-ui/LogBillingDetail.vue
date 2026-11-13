<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { logEvidence } from '@/utils/logModel'
import type { LogItem } from '@/types/console'
import { formatQuota } from '@/utils/format'
const props = defineProps<{ log: LogItem }>(),
  { t } = useI18n()
const record = computed(() => {
  const data = logEvidence(props.log).request_plugins
  return Array.isArray(data) ? data : []
})
const details = computed(() => logEvidence(props.log))
</script>
<template>
  <div class="space-y-4 text-left">
    <dl class="grid grid-cols-[auto_1fr] gap-3 text-sm">
      <dt>{{ t('billing.clientModel') }}</dt>
      <dd class="break-all font-mono">{{ log.model }}</dd>
      <dt>{{ t('billing.finalCharge') }}</dt>
      <dd>{{ formatQuota(log.quota) }}</dd>
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
