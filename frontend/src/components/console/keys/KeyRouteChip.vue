<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

import StatusChip from '@/components/common/StatusChip.vue'
import type { TokenSummary } from '@/types/console'

const props = withDefaults(
  defineProps<{
    token: Pick<
      TokenSummary,
      | 'name'
      | 'type'
      | 'selected_only'
      | 'route_channel_count'
      | 'enabled_route_channel_count'
      | 'route_scope'
      | 'effective_mode'
    >
    interactive?: boolean
  }>(),
  { interactive: false }
)

const emit = defineEmits<{
  configure: []
}>()

const { t } = useI18n()

const enabledCount = computed(
  () =>
    props.token.enabled_route_channel_count ?? props.token.route_channel_count
)

const scope = computed<'platform' | 'selected' | 'empty'>(() => {
  if (props.token.route_scope) return props.token.route_scope
  if (!props.token.selected_only) return 'platform'
  if (props.token.route_channel_count === 0) return 'empty'
  if (enabledCount.value === 0) return 'platform'
  return 'selected'
})

const effectiveMode = computed<'auto' | 'ordered'>(() => {
  if (props.token.effective_mode) return props.token.effective_mode
  return scope.value === 'selected' && props.token.type === 'ordered'
    ? 'ordered'
    : 'auto'
})

const unavailable = computed(() => scope.value === 'empty')

const label = computed(() => {
  if (scope.value === 'empty') return t('keys.route.unavailable')
  if (scope.value === 'platform') {
    if (props.token.route_channel_count > 0) {
      return t('keys.route.autoPlatformPaused', {
        count: props.token.route_channel_count,
      })
    }
    return t('keys.route.autoPlatform')
  }
  const key =
    effectiveMode.value === 'ordered'
      ? 'keys.route.ordered'
      : 'keys.route.autoSelected'
  return t(key, { count: enabledCount.value }, enabledCount.value)
})

const tone = computed(() => {
  if (unavailable.value) return 'warning'
  return effectiveMode.value === 'ordered' ? 'accent' : 'info'
})

const ariaLabel = computed(() =>
  t('keys.route.editAria', {
    name: props.token.name || '',
    label: label.value,
  })
)
</script>

<template>
  <button
    v-if="interactive"
    type="button"
    class="key-route-chip-trigger relative inline-flex max-w-full cursor-pointer items-center rounded-lg text-left transition-opacity hover:opacity-85 focus-ring"
    :aria-label="ariaLabel"
    :title="unavailable ? t('keys.route.unavailableHint') : ariaLabel"
    data-key-route-trigger
    @click.stop="emit('configure')"
  >
    <StatusChip
      class="key-route-chip relative"
      :tone="tone"
      :data-route-warning="unavailable ? '' : undefined"
    >
      {{ label
      }}<span v-if="unavailable" class="sr-only">{{
        t('keys.route.unavailableHint')
      }}</span>
    </StatusChip>
  </button>
  <StatusChip
    v-else
    class="key-route-chip relative"
    :tone="tone"
    :data-route-warning="unavailable ? '' : undefined"
    :title="unavailable ? t('keys.route.unavailableHint') : undefined"
  >
    {{ label
    }}<span v-if="unavailable" class="sr-only">{{
      t('keys.route.unavailableHint')
    }}</span>
  </StatusChip>
</template>

<style scoped>
/* B1 Studio: route-type labels are auxiliary and stay neutral across the table,
   mobile cards, and teleported popover; deleted-channel warnings keep status
   colour. !important overrides the chip's inline tone style. */
:global(html.dark .key-route-chip:not([data-route-warning])) {
  background: var(--surface-muted) !important;
  color: var(--text-secondary) !important;
  border-left-color: var(--border-default) !important;
}

/* Guarantee a 44x44px touch target on mobile and coarse pointers without
   expanding the visual chip height. */
@media (max-width: 1023px), (pointer: coarse) {
  .key-route-chip-trigger::after {
    content: '';
    position: absolute;
    inset: -10px -6px;
    min-height: 44px;
    min-width: 44px;
  }
}
</style>
