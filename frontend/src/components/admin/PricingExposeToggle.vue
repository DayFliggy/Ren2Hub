<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { modelPricingMessages } from '@/i18n/modelPricing'
import { useSystemSettings } from '@/composables/useSystemSettings'
// The saved setting is the only state: a failed save leaves it unchanged, so
// the checkbox is put back to match it.
const { t } = useI18n({ useScope: 'local', messages: modelPricingMessages })
const settings = useSystemSettings()
const enabled = computed(() =>
  ['true', '1'].includes(String(settings.rawValue('ExposeRatioEnabled', false)))
)
async function change(event: Event) {
  const input = event.target as HTMLInputElement
  if (!(await settings.saveOptions({ ExposeRatioEnabled: input.checked })))
    input.checked = enabled.value
}
</script>
<template>
  <label
    class="inline-flex min-h-11 items-center gap-2 cursor-pointer sm:min-h-0 text-xs text-[var(--text-secondary)] select-none"
    :title="t('exposeHint')"
  >
    <input
      type="checkbox"
      class="checkbox-round"
      :checked="enabled"
      @change="change"
    />
    <span>{{ t('expose') }}</span>
  </label>
</template>
