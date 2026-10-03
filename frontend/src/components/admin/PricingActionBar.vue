<script setup lang="ts">
/** Page-header actions for model pricing: draft marker, reload, fetch, save. */
import { useI18n } from 'vue-i18n'
import { RefreshCw, Save, Sparkles } from 'lucide-vue-next'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import { modelPricingMessages } from '@/i18n/modelPricing'
import type { PricingBarState } from './pricingActionBar'

defineProps<{ state: PricingBarState }>()
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
</script>
<template>
  <div class="flex flex-wrap items-center gap-2">
    <span
      v-if="state.dirty"
      class="inline-flex items-center gap-1.5 text-xs font-medium text-[var(--status-warning-text)]"
      role="status"
    >
      <span
        class="h-2 w-2 rounded-full bg-[var(--status-warning)] shadow-[0_0_0_2px_var(--status-warning-soft)]"
        aria-hidden="true"
      />
      {{ t('dirty') }}
    </span>
    <ConsoleButton
      variant="ghost"
      size="sm"
      :loading="state.loading"
      :disabled="state.saving || state.settingsSaving"
      @click="state.reload"
    >
      <RefreshCw :size="13" class="mr-1.5" aria-hidden="true" />{{
        t('reload')
      }}
    </ConsoleButton>
    <template v-if="state.tab !== 'tools'">
      <ConsoleButton
        variant="secondary"
        size="sm"
        :disabled="state.loading || state.saving || !state.ready"
        @click="state.beginFetch"
      >
        <Sparkles :size="13" class="mr-1.5" aria-hidden="true" />{{
          t('fetch')
        }}
      </ConsoleButton>
      <ConsoleButton
        size="sm"
        :loading="state.saving"
        :disabled="state.loading || !state.ready"
        @click="state.save"
      >
        <Save :size="13" class="mr-1.5" aria-hidden="true" />{{ t('save') }}
      </ConsoleButton>
    </template>
  </div>
</template>
