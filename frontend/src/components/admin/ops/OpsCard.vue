<script setup lang="ts">
import { computed } from 'vue'
import { AlertTriangle } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import { opsMonitorMessages } from '@/i18n/opsMonitor'
import { formatClock } from '@/utils/opsFormat'

/**
 * Card shell shared by every ops section: title with a measurement-scope
 * hint, skeleton before the first load, and an explicit stale marker when a
 * refresh fails after data was shown. Cards fed by the page-level overview
 * pass `retryable: false`; the status header owns that retry and error text.
 */
const props = withDefaults(
  defineProps<{
    title: string
    scope?: string
    scopeHint?: string
    loading: boolean
    loaded: boolean
    error: string
    updatedAt: number
    retryable?: boolean
    stretch?: boolean
  }>(),
  { scope: '', scopeHint: '', retryable: true, stretch: true }
)
const emit = defineEmits<{ retry: [] }>()
const { t, locale } = useI18n({
  useScope: 'local',
  messages: opsMonitorMessages,
})
const lastSuccess = computed(() =>
  props.updatedAt
    ? t('lastSuccess', { time: formatClock(props.updatedAt, locale.value) })
    : t('neverLoaded')
)
</script>

<template>
  <ConsoleCard
    class="ops-card"
    :title="title"
    :padded="false"
    :stretch="stretch"
    :aria-busy="loading"
    :data-ops-stale="loaded && error ? 'true' : undefined"
  >
    <template #title>
      <div class="ops-card-title">
        <h2>{{ title }}</h2>
        <span v-if="scope" class="ops-scope" :title="scopeHint || undefined">{{
          scope
        }}</span>
        <span v-if="loaded && error" class="ops-stale-chip">{{
          t('stale')
        }}</span>
      </div>
    </template>
    <template v-if="$slots.action" #action><slot name="action" /></template>
    <div class="ops-card-body">
      <div
        v-if="error && retryable"
        class="ops-inline-error"
        role="alert"
        data-ops-error
      >
        <AlertTriangle :size="15" aria-hidden="true" />
        <div class="min-w-0 flex-1">
          <p class="ops-inline-error-title">
            {{ loaded ? t('stale') : t('loadFailed') }}
          </p>
          <p class="ops-inline-error-detail">{{ error }}</p>
          <p class="ops-inline-error-detail">{{ lastSuccess }}</p>
        </div>
        <ConsoleButton
          variant="secondary"
          size="sm"
          :loading="loading"
          @click="emit('retry')"
          >{{ t('retry') }}</ConsoleButton
        >
      </div>
      <p v-else-if="error && loaded" class="ops-stale-note">
        {{ t('staleHint', { time: formatClock(updatedAt, locale) || '--' }) }}
      </p>
      <div
        v-if="loaded"
        class="ops-card-content"
        :class="{ 'ops-card-stale': Boolean(error) }"
      >
        <slot />
      </div>
      <p v-else-if="error && !retryable" class="ops-empty">
        {{ t('unavailable') }}
      </p>
      <div
        v-else-if="!error"
        class="ops-skeleton"
        role="status"
        :aria-label="t('loading')"
      >
        <SkeletonBlock rounded="lg" class="h-16" />
        <SkeletonBlock rounded="lg" class="h-24" />
      </div>
    </div>
  </ConsoleCard>
</template>
