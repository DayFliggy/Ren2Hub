<script setup lang="ts">
import { computed } from 'vue'

import KeyMeterNote from '@/components/console/keys/KeyMeterNote.vue'
import type { TokenSummary } from '@/types/console'
import { tokenMeter } from '@/utils/tokenMeter'

/** Name plus the key's state line, shared by table rows and mobile cards. */
const props = withDefaults(
  defineProps<{
    token: TokenSummary
    now: number
    heading?: boolean
  }>(),
  { heading: false }
)

const meter = computed(() => tokenMeter(props.token, props.now))
</script>

<template>
  <div class="min-w-0" data-key-identity :data-key-state="meter.state">
    <component
      :is="heading ? 'h2' : 'span'"
      class="block truncate"
      :class="
        heading
          ? 'text-sm font-semibold text-[var(--text-primary)]'
          : 'font-medium'
      "
      :title="token.name"
    >
      {{ token.name }}
    </component>
    <KeyMeterNote
      class="mt-0.5 block truncate text-[0.6875rem] leading-4"
      :token="token"
      :meter="meter"
    />
  </div>
</template>
