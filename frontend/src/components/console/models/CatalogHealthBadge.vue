<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type {
  CatalogChannelHealth,
  CatalogOfferHealth,
} from '@/api/modelCatalog'
import { modelMarketMessages } from '@/i18n/modelMarket'
import {
  resolveOfferHealth,
  type CatalogHealthIcon as HealthIcon,
  type CatalogHealthTone as HealthTone,
} from './catalogHealth'

const props = withDefaults(
  defineProps<{
    kind?: 'offer' | 'channel'
    health?: CatalogOfferHealth
    channelSummary?: CatalogChannelHealth
    /** Number of models currently shown after client filters; when below visible_models, show visible scope note. */
    filteredCount?: number
    compact?: boolean
    /** Compact corner badge beside billing mode on model aggregation cards. */
    badge?: boolean
    detail?: boolean
  }>(),
  {
    kind: 'offer',
    health: undefined,
    channelSummary: undefined,
    filteredCount: undefined,
    compact: false,
    badge: false,
    detail: false,
  }
)

const { t } = useI18n({ useScope: 'local', messages: modelMarketMessages })

const offerView = computed(() => resolveOfferHealth(props.health, t))

const offerFullLabel = computed(() =>
  offerView.value.secondary
    ? `${offerView.value.primary} · ${offerView.value.secondary}`
    : offerView.value.primary
)

const summaryView = computed(() => {
  const s = props.channelSummary
  if (!s) {
    return {
      state: 'unavailable',
      tone: 'neutral' as HealthTone,
      icon: 'dash' as HealthIcon,
      text: t('channelHealth.unavailable'),
      hint: '',
    }
  }
  const hint = t('channelHealth.summary.scopeHint', { count: s.visible_models })
  if (
    s.visible_models === 0 ||
    (s.affected_models === 0 && s.evaluated_models === 0)
  ) {
    return {
      state: 'unrated',
      tone: 'neutral' as HealthTone,
      icon: 'dash' as HealthIcon,
      text: t('channelHealth.summary.allUnrated'),
      hint,
    }
  }
  if (s.affected_models === 0 && s.unrated_models === 0) {
    return {
      state: 'clean',
      tone: 'good' as HealthTone,
      icon: 'check' as HealthIcon,
      text: t('channelHealth.summary.allClean'),
      hint,
    }
  }
  const parts: string[] = []
  if (s.affected_models > 0) {
    parts.push(
      t('channelHealth.summary.affected', { count: s.affected_models })
    )
  }
  if (s.evaluated_models > 0) {
    parts.push(
      t('channelHealth.summary.evaluated', { count: s.evaluated_models })
    )
  }
  if (s.unrated_models > 0) {
    parts.push(t('channelHealth.summary.unrated', { count: s.unrated_models }))
  }
  return {
    state: s.affected_models > 0 ? 'affected' : 'partial',
    tone: (s.affected_models > 0 ? 'warn' : 'neutral') as HealthTone,
    icon: (s.affected_models > 0 ? 'limit' : 'dash') as HealthIcon,
    text: parts.join(' · '),
    hint,
  }
})

const showFilteredScope = computed(
  () =>
    props.channelSummary !== undefined &&
    props.filteredCount !== undefined &&
    props.filteredCount < props.channelSummary.visible_models
)
const filteredScopeText = computed(() =>
  showFilteredScope.value && props.channelSummary
    ? t('channelHealth.summary.filteredScope', {
        count: props.channelSummary.visible_models,
      })
    : ''
)
</script>

<template>
  <!-- Channel-level summary (used in shelf header) -->
  <div
    v-if="kind === 'channel' || channelSummary !== undefined"
    class="catalog-health-summary"
    :class="[`is-${summaryView.tone}`, { compact }]"
    :data-catalog-channel-health="summaryView.state"
    :aria-label="
      summaryView.hint
        ? `${t('channelHealth.summary.prefix')}：${summaryView.text}（${summaryView.hint}）`
        : undefined
    "
    :title="summaryView.hint || undefined"
  >
    <span class="catalog-health-prefix">{{
      t('channelHealth.summary.prefix')
    }}</span>
    <span class="catalog-health-pill" :class="`is-${summaryView.tone}`">
      <svg
        width="12"
        height="12"
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        <path
          v-if="summaryView.icon === 'check'"
          d="M3.5 8.5 6.5 11.5 12.5 4.5"
        />
        <path
          v-else-if="summaryView.icon === 'limit'"
          d="M8 3.5v5m0 3h.01M8 1.75a6.25 6.25 0 1 1 0 12.5 6.25 6.25 0 0 1 0-12.5Z"
        />
        <path v-else d="M4 8h8" />
      </svg>
      <span>{{ summaryView.text }}</span>
    </span>
    <span
      v-if="filteredScopeText"
      class="catalog-health-scope-note"
      data-catalog-channel-scope
    >
      {{ filteredScopeText }}
    </span>
  </div>

  <!-- Detail-mode offer health block (used in ModelMarketDetails right panel) -->
  <div
    v-else-if="detail"
    class="catalog-health-detail"
    :data-catalog-offer-health="offerView.state"
  >
    <span class="catalog-health-detail-title">{{
      t('channelHealth.label')
    }}</span>
    <div class="catalog-health-detail-body">
      <span class="catalog-health-pill" :class="`is-${offerView.tone}`">
        <svg
          width="12"
          height="12"
          viewBox="0 0 16 16"
          fill="none"
          stroke="currentColor"
          stroke-width="1.8"
          stroke-linecap="round"
          stroke-linejoin="round"
          aria-hidden="true"
        >
          <path
            v-if="offerView.icon === 'check'"
            d="M3.5 8.5 6.5 11.5 12.5 4.5"
          />
          <path
            v-else-if="offerView.icon === 'pause'"
            d="M5.5 3.5v9M10.5 3.5v9"
          />
          <path
            v-else-if="offerView.icon === 'limit'"
            d="M8 3.5v5m0 3h.01M8 1.75a6.25 6.25 0 1 1 0 12.5 6.25 6.25 0 0 1 0-12.5Z"
          />
          <path
            v-else-if="offerView.icon === 'recover'"
            d="M12.5 8a4.5 4.5 0 1 1-1.3-3.2M12.5 3v2.5H10"
          />
          <path v-else-if="offerView.icon === 'down'" d="M4 6l4 4 4-4" />
          <path v-else d="M4 8h8" />
        </svg>
        <span>{{ offerView.primary }}</span>
      </span>
      <span v-if="offerView.secondary" class="catalog-health-secondary">
        {{ offerView.secondary }}
      </span>
    </div>
    <p class="catalog-health-detail-note">{{ t('channelHealth.scopeNote') }}</p>
  </div>

  <!-- Compact corner badge (inline beside billing mode on ModelCard) -->
  <span
    v-else-if="badge"
    class="catalog-health-pill catalog-health-badge"
    :class="`is-${offerView.tone}`"
    :data-catalog-offer-health="offerView.state"
    :aria-label="offerFullLabel"
  >
    <svg
      width="10"
      height="10"
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="2"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path v-if="offerView.icon === 'check'" d="M3.5 8.5 6.5 11.5 12.5 4.5" />
      <path v-else-if="offerView.icon === 'pause'" d="M5.5 3.5v9M10.5 3.5v9" />
      <path
        v-else-if="offerView.icon === 'limit'"
        d="M8 3.5v5m0 3h.01M8 1.75a6.25 6.25 0 1 1 0 12.5 6.25 6.25 0 0 1 0-12.5Z"
      />
      <path
        v-else-if="offerView.icon === 'recover'"
        d="M12.5 8a4.5 4.5 0 1 1-1.3-3.2M12.5 3v2.5H10"
      />
      <path v-else-if="offerView.icon === 'down'" d="M4 6l4 4 4-4" />
      <path v-else d="M4 8h8" />
    </svg>
    <span>{{ offerView.badge }}</span>
  </span>

  <!-- Compact inline offer badge (shelves, matrix, detail rows) -->
  <span
    v-else
    class="catalog-health-inline"
    :class="{ compact }"
    :data-catalog-offer-health="offerView.state"
  >
    <span class="catalog-health-pill" :class="`is-${offerView.tone}`">
      <svg
        width="11"
        height="11"
        viewBox="0 0 16 16"
        fill="none"
        stroke="currentColor"
        stroke-width="1.8"
        stroke-linecap="round"
        stroke-linejoin="round"
        aria-hidden="true"
      >
        <path
          v-if="offerView.icon === 'check'"
          d="M3.5 8.5 6.5 11.5 12.5 4.5"
        />
        <path
          v-else-if="offerView.icon === 'pause'"
          d="M5.5 3.5v9M10.5 3.5v9"
        />
        <path
          v-else-if="offerView.icon === 'limit'"
          d="M8 3.5v5m0 3h.01M8 1.75a6.25 6.25 0 1 1 0 12.5 6.25 6.25 0 0 1 0-12.5Z"
        />
        <path
          v-else-if="offerView.icon === 'recover'"
          d="M12.5 8a4.5 4.5 0 1 1-1.3-3.2M12.5 3v2.5H10"
        />
        <path v-else-if="offerView.icon === 'down'" d="M4 6l4 4 4-4" />
        <path v-else d="M4 8h8" />
      </svg>
      <span>{{ offerView.primary }}</span>
    </span>
    <span v-if="offerView.secondary" class="catalog-health-secondary">
      {{ offerView.secondary }}
    </span>
  </span>
</template>

<style scoped>
.catalog-health-summary {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.4rem 0.5rem;
  font-size: 0.75rem;
  color: var(--text-secondary);
}
.catalog-health-prefix,
.catalog-health-scope-note {
  font-size: 0.6875rem;
  color: var(--text-tertiary);
}
.catalog-health-inline {
  display: inline-flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.3rem 0.45rem;
  max-width: 100%;
  font-size: 0.6875rem;
  line-height: 1.35;
}
.catalog-health-pill {
  display: inline-flex;
  align-items: center;
  gap: 0.28rem;
  padding: 0.14rem 0.45rem;
  border-radius: var(--shape-control);
  border: 1px solid var(--border-subtle);
  background: var(--surface-muted);
  color: var(--text-secondary);
  font-weight: 500;
  max-width: 100%;
  overflow-wrap: anywhere;
}
.catalog-health-pill svg {
  flex: none;
}
.catalog-health-badge {
  padding: 0.08rem 0.36rem;
  gap: 0.22rem;
  font-size: 0.625rem;
  line-height: 1.35;
  white-space: nowrap;
  flex: none;
}
.catalog-health-pill.is-good {
  border-color: color-mix(in srgb, var(--status-success) 45%, transparent);
  background: var(--status-success-soft);
  color: var(--status-success-text);
}
.catalog-health-pill.is-warn {
  border-color: color-mix(in srgb, var(--status-warning) 48%, transparent);
  background: var(--status-warning-soft);
  color: var(--status-warning-text);
}
.catalog-health-pill.is-bad {
  border-color: color-mix(in srgb, var(--status-danger) 48%, transparent);
  background: var(--status-danger-soft);
  color: var(--status-danger-text);
}
.catalog-health-pill.is-neutral {
  border: 1px dashed var(--border-default);
  background: var(--surface-solid);
  color: var(--text-secondary);
}
.catalog-health-pill.is-neutral svg {
  color: var(--load-unchecked);
}
html.dark .catalog-health-pill.is-neutral {
  border-color: color-mix(in srgb, var(--load-unchecked) 38%, transparent);
  background: color-mix(in srgb, var(--load-unchecked) 10%, transparent);
  color: var(--text-secondary);
}
.catalog-health-secondary {
  font-size: 0.6875rem;
  color: var(--status-warning-text);
}
.catalog-health-detail {
  display: grid;
  gap: 0.45rem;
  padding: 0.75rem 0;
  border-top: 1px dashed var(--border-subtle);
  font-size: 0.75rem;
}
.catalog-health-detail-title {
  color: var(--text-tertiary);
  font-size: 0.75rem;
}
.catalog-health-detail-body {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 0.4rem 0.55rem;
}
.catalog-health-detail-note {
  color: var(--text-tertiary);
  font-size: 0.6875rem;
  line-height: 1.5;
}
html.dark .catalog-health-detail {
  border-top-style: solid;
}
</style>
