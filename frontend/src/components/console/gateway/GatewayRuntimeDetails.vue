<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GatewayRuntime } from '@/types/gateway'

const props = defineProps<{ runtime: GatewayRuntime }>()
const { t, locale } = useI18n()
const startedAt = computed(() =>
  new Date(props.runtime.started_at * 1000).toLocaleString(locale.value)
)
</script>

<template>
  <div class="sibling-space-y-2 text-xs text-[var(--text-tertiary)]">
    <p
      v-if="runtime.draining"
      role="status"
      class="text-[var(--status-warning-text)]"
    >
      {{ t('gateway.draining') }}
    </p>
    <dl class="flex flex-wrap gap-x-5 gap-y-2">
      <div class="flex gap-1">
        <dt>{{ t('gateway.configVersion') }}</dt>
        <dd>{{ runtime.config_version }}</dd>
      </div>
      <div class="flex gap-1">
        <dt>{{ t('gateway.runtimeRevision') }}</dt>
        <dd>{{ runtime.revision }}</dd>
      </div>
      <div class="flex flex-wrap gap-1">
        <dt>{{ t('gateway.startedAt') }}</dt>
        <dd class="break-words">{{ startedAt }}</dd>
      </div>
    </dl>
  </div>
</template>
