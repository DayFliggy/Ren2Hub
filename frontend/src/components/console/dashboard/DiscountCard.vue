<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { Settings2, TriangleAlert } from 'lucide-vue-next'

import ConsoleCard from '@/components/common/ConsoleCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import { quotaToAmount, rateToPercent } from '@/features/usage-discount/amount'
import type { UsageDiscountSelf } from '@/types/usageDiscount'
import { compareDecimal } from '@/utils/decimal'
import { quotaRatio } from '@/utils/quota'

const RULER_SEGMENTS = 7

type RulerSegmentKind = 'global' | 'personal' | 'overflow' | 'empty'

const props = withDefaults(
  defineProps<{
    discounts: UsageDiscountSelf | null
    loading?: boolean
    error?: string | null
    canConfigure?: boolean
  }>(),
  { loading: false, error: null, canConfigure: false }
)

const { t } = useI18n()

// Rates have at most four decimal places. Restore that precision only for
// this display subtraction; the server's final factor is rendered unchanged.
const globalRate = computed(() =>
  Math.max(0, 1 - Number(props.discounts?.global_factor ?? '1')).toFixed(4)
)

const winner = computed(() =>
  props.discounts?.windows.find(
    (window) => window.window === props.discounts?.winner_window
  )
)

const rawTotalRate = computed(() => {
  const fixed = Number(props.discounts?.fixed_rate ?? '0')
  const site = Number(props.discounts?.site_rate ?? '0')
  const personal = Number(winner.value?.rate ?? '0')
  return Math.max(0, fixed + site + personal).toFixed(4)
})

/**
 * Allocates the 7 optical water-level segments by exact integer counts so any
 * positive rate lights at least one segment and the cap marker aligns cleanly
 * with the boundary between effective and overflow segments.
 */
const rulerModel = computed(() => {
  const applied = Number(props.discounts?.applied_rate ?? '0')
  const global = Number(globalRate.value)
  const raw = Number(rawTotalRate.value)
  const cap = Number(props.discounts?.cap_rate ?? '0')
  const scaleMax = Math.max(raw, cap, 0.05)

  if (applied <= 0 && raw <= 0) {
    return {
      segments: new Array<RulerSegmentKind>(RULER_SEGMENTS).fill('empty'),
      capPercent: cap > 0 ? Math.min(100, (cap / scaleMax) * 100) : null,
    }
  }

  const effectiveGlobal = Math.min(global, applied > 0 ? applied : global)
  const effectivePersonal = Math.max(0, applied - effectiveGlobal)
  const overflow = cap > 0 ? Math.max(0, raw - cap) : 0

  let globalCount =
    effectiveGlobal > 0
      ? Math.max(1, Math.round((effectiveGlobal / scaleMax) * RULER_SEGMENTS))
      : 0
  let personalCount =
    effectivePersonal > 0
      ? Math.max(1, Math.round((effectivePersonal / scaleMax) * RULER_SEGMENTS))
      : 0
  let overflowCount =
    overflow > 0
      ? Math.max(1, Math.round((overflow / scaleMax) * RULER_SEGMENTS))
      : 0

  while (
    globalCount + personalCount + overflowCount > RULER_SEGMENTS &&
    (globalCount > 1 || personalCount > 1 || overflowCount > 1)
  ) {
    if (
      globalCount >= personalCount &&
      globalCount >= overflowCount &&
      globalCount > 1
    ) {
      globalCount -= 1
    } else if (personalCount >= overflowCount && personalCount > 1) {
      personalCount -= 1
    } else if (overflowCount > 1) {
      overflowCount -= 1
    } else {
      break
    }
  }

  const emptyCount = Math.max(
    0,
    RULER_SEGMENTS - globalCount - personalCount - overflowCount
  )
  const segments: RulerSegmentKind[] = [
    ...new Array<RulerSegmentKind>(globalCount).fill('global'),
    ...new Array<RulerSegmentKind>(personalCount).fill('personal'),
    ...new Array<RulerSegmentKind>(overflowCount).fill('overflow'),
    ...new Array<RulerSegmentKind>(emptyCount).fill('empty'),
  ].slice(0, RULER_SEGMENTS)

  let capPercent: number | null = null
  if (cap > 0) {
    capPercent =
      overflowCount > 0
        ? ((RULER_SEGMENTS - overflowCount) / RULER_SEGMENTS) * 100
        : Math.min(100, (cap / scaleMax) * 100)
  }

  return { segments, capPercent }
})

const windows = computed(() => {
  const list = props.discounts?.windows ?? []
  const maxRate = Math.max(
    ...list.flatMap((w) => [
      Number(w.rate),
      ...w.tiers.map((tier) => Number(tier.rate)),
    ]),
    0
  )
  return list.map((window) => {
    // Equal-rate tiers do not offer a larger discount. This is a display
    // target, not a prediction: rolling windows are recomputed each day.
    const next = window.tiers.find(
      (tier) =>
        BigInt(tier.threshold_quota) > BigInt(window.quota) &&
        compareDecimal(tier.rate, window.rate) > 0
    )
    const rateNum = Number(window.rate)
    let fillPercent = 0
    if (maxRate > 0 && rateNum > 0) {
      fillPercent = Math.min(100, Math.round((rateNum / maxRate) * 100))
    } else if (next && BigInt(next.threshold_quota) > 0n) {
      fillPercent = Math.min(
        100,
        Math.round(quotaRatio(window.quota, next.threshold_quota) * 100)
      )
    }
    return {
      ...window,
      next,
      gap: next
        ? (BigInt(next.threshold_quota) - BigInt(window.quota)).toString()
        : null,
      fillPercent,
    }
  })
})

function discountLabel(rate: string): string {
  return Number(rate) === 0
    ? t('dashboard.discount.noDiscount')
    : t('dashboard.discount.rateOff', { rate: rateToPercent(rate) })
}
</script>

<template>
  <ConsoleCard
    :title="t('dashboard.discount.title')"
    stretch
    :padded="false"
    class="discount-glass-card"
    data-discount-card
  >
    <template #action>
      <div class="flex items-center gap-1.5">
        <span
          v-if="discounts?.cap_applied"
          class="discount-cap-pill font-semibold"
          data-discount-cap
        >
          {{
            t('dashboard.discount.capped', {
              rate: rateToPercent(discounts.cap_rate),
            })
          }}
        </span>
        <RouterLink
          v-if="canConfigure"
          to="/system-settings/billing/usage-discount"
          class="-mr-2 inline-flex h-11 w-11 shrink-0 items-center justify-center text-[var(--text-secondary)] transition-colors hover:text-[var(--text-primary)] focus-ring touch-snappy sm:-mr-1 sm:h-8 sm:w-8"
          :aria-label="t('dashboard.discount.configure')"
          :title="t('dashboard.discount.configure')"
          data-handdrawn="control"
          data-discount-configure
        >
          <span class="discount-config-orb" aria-hidden="true">
            <Settings2 :size="14" />
          </span>
        </RouterLink>
      </div>
    </template>

    <div
      v-if="loading"
      class="flex grow flex-col gap-3 px-4 pt-2.5 pb-3.5 sm:px-5"
      aria-busy="true"
    >
      <SkeletonBlock rounded="md" class="h-28" />
      <SkeletonBlock rounded="md" class="h-36" />
    </div>

    <div
      v-else-if="discounts"
      class="discount-body flex grow flex-col justify-between gap-2.5 px-4 pt-2 pb-3.5 sm:px-5"
    >
      <div class="glass-chamber">
        <div class="chamber-hero" data-discount-effective>
          <div class="min-w-0">
            <p class="text-[11px] leading-tight text-[var(--text-secondary)]">
              {{ t('dashboard.discount.effective') }}
            </p>
            <div class="mt-1 flex flex-wrap items-baseline gap-1.5">
              <p
                class="display-number font-mono text-[1.75rem] leading-none tracking-tight"
                :class="
                  Number(discounts.applied_rate) > 0
                    ? 'text-[var(--accent-text)]'
                    : 'text-[var(--text-primary)]'
                "
                data-discount-factor
              >
                {{ discounts.factor }}×
              </p>
              <span
                class="font-mono text-[11px] font-bold whitespace-nowrap tabular-nums"
                :class="
                  Number(discounts.applied_rate) > 0
                    ? 'text-[var(--text-primary)]'
                    : 'text-[var(--text-tertiary)]'
                "
                data-discount-applied
              >
                {{ discountLabel(discounts.applied_rate) }}
              </span>
            </div>
          </div>

          <div class="min-w-0">
            <div
              class="mb-1.5 flex items-baseline justify-between gap-2 font-mono text-[10px] font-medium whitespace-nowrap text-[var(--text-secondary)]"
            >
              <span>
                {{
                  t('dashboard.discount.stackTotal', {
                    rate: rateToPercent(rawTotalRate),
                  })
                }}
              </span>
              <span v-if="Number(discounts.cap_rate) > 0">
                {{
                  t('dashboard.discount.capLimit', {
                    rate: rateToPercent(discounts.cap_rate),
                  })
                }}
              </span>
            </div>
            <div
              class="ruler-track"
              role="img"
              :aria-label="
                t('dashboard.discount.rulerAria', {
                  global: rateToPercent(globalRate),
                  personal: rateToPercent(winner?.rate ?? '0'),
                  cap: rateToPercent(discounts.cap_rate),
                })
              "
              data-discount-ruler
            >
              <span
                v-for="(seg, idx) in rulerModel.segments"
                :key="idx"
                class="ruler-seg"
                :class="`ruler-seg--${seg}`"
              />
              <span
                v-if="rulerModel.capPercent !== null"
                class="cap-marker"
                :style="{ left: `calc(${rulerModel.capPercent}% - 1px)` }"
                aria-hidden="true"
              />
            </div>
          </div>
        </div>

        <div class="chamber-ledger">
          <div class="ledger-row" data-discount-global>
            <div class="flex min-w-0 items-center gap-1.5">
              <span
                class="ruler-swatch ruler-swatch--global"
                aria-hidden="true"
              />
              <h3 class="shrink-0 text-[11px] font-semibold whitespace-nowrap">
                {{ t('dashboard.discount.global') }}
              </h3>
              <p
                class="min-w-0 truncate font-mono text-[10px] text-[var(--text-secondary)]"
                data-discount-global-parts
              >
                {{
                  t('dashboard.discount.globalParts', {
                    fixed: rateToPercent(discounts.fixed_rate),
                    site: rateToPercent(discounts.site_rate),
                  })
                }}
              </p>
            </div>
            <span
              class="shrink-0 font-mono text-[11px] font-bold whitespace-nowrap tabular-nums"
              :class="
                Number(globalRate) > 0
                  ? 'text-[var(--accent-text)]'
                  : 'text-[var(--text-tertiary)]'
              "
              data-discount-global-rate
            >
              {{ discountLabel(globalRate) }}
            </span>
          </div>

          <div class="ledger-row">
            <div class="flex min-w-0 items-center gap-1.5">
              <span
                class="ruler-swatch ruler-swatch--personal"
                aria-hidden="true"
              />
              <h3 class="shrink-0 text-[11px] font-semibold whitespace-nowrap">
                {{ t('dashboard.discount.personal') }}
              </h3>
              <p
                class="min-w-0 truncate text-[10px] text-[var(--text-secondary)]"
              >
                {{ t('dashboard.discount.personalHint') }}
              </p>
            </div>
            <span
              class="shrink-0 font-mono text-[11px] font-bold whitespace-nowrap tabular-nums"
              :class="
                winner && Number(winner.rate) > 0
                  ? 'text-[var(--accent-text)]'
                  : 'text-[var(--text-tertiary)]'
              "
              data-discount-personal-rate
            >
              {{ windows.length ? discountLabel(winner?.rate ?? '0') : '—' }}
            </span>
          </div>
        </div>
      </div>

      <p
        v-if="discounts.status === 'unavailable'"
        class="flex items-start gap-2 bg-[var(--status-warning-soft)] px-3 py-1.5 text-xs leading-snug text-[var(--status-warning-text)]"
        style="border-radius: var(--shape-control)"
        role="status"
        data-discount-unavailable
      >
        <TriangleAlert :size="14" class="mt-0.5 shrink-0" aria-hidden="true" />
        {{ t('dashboard.discount.unavailable') }}
      </p>

      <div data-discount-personal>
        <ul
          v-if="windows.length"
          class="window-rows"
          :aria-label="t('dashboard.discount.personal')"
        >
          <li
            v-for="window in windows"
            :key="window.window"
            class="wrow"
            :class="
              window.window === discounts.winner_window
                ? 'wrow--selected'
                : 'wrow--idle'
            "
            :data-discount-window="window.window"
            :data-selected="window.window === discounts.winner_window"
            :aria-current="
              window.window === discounts.winner_window ? 'true' : undefined
            "
          >
            <span
              class="text-xs font-semibold whitespace-nowrap"
              :class="
                window.window === discounts.winner_window
                  ? 'text-[var(--accent-text)]'
                  : 'text-[var(--text-primary)]'
              "
            >
              {{ t(`dashboard.discount.windows.${window.window}`) }}
            </span>

            <div class="wrow-mid" :data-discount-progress="window.window">
              <div
                class="flex items-baseline justify-between gap-1.5 text-[10px] whitespace-nowrap"
              >
                <p
                  class="tabular-nums"
                  :class="
                    window.window === discounts.winner_window
                      ? 'text-[var(--accent-text)]'
                      : 'text-[var(--text-secondary)]'
                  "
                >
                  {{
                    t('dashboard.discount.windowUsage', {
                      amount: quotaToAmount(window.quota),
                    })
                  }}
                </p>
                <p
                  v-if="window.next && window.gap !== null"
                  class="tabular-nums"
                  :class="
                    window.window === discounts.winner_window
                      ? 'text-[var(--accent-text)]'
                      : 'text-[var(--text-secondary)]'
                  "
                >
                  {{
                    t('dashboard.discount.nextTier', {
                      amount: quotaToAmount(window.gap),
                      rate: rateToPercent(window.next.rate),
                    })
                  }}
                </p>
                <p
                  v-else
                  :class="
                    window.window === discounts.winner_window
                      ? 'text-[var(--accent-text)]'
                      : 'text-[var(--text-tertiary)]'
                  "
                >
                  {{ t('dashboard.discount.noHigherTier') }}
                </p>
              </div>
              <div class="bar-track" aria-hidden="true">
                <div
                  class="bar-fill"
                  :style="{ width: `${window.fillPercent}%` }"
                />
              </div>
            </div>

            <span
              class="text-right font-mono text-base font-bold whitespace-nowrap tabular-nums"
              :class="
                window.window === discounts.winner_window
                  ? 'text-[var(--accent-text)]'
                  : 'text-[var(--text-primary)]'
              "
            >
              {{ rateToPercent(window.rate) }}%
            </span>
          </li>
        </ul>

        <p
          v-else
          class="my-auto py-6 text-center text-xs leading-relaxed text-[var(--text-tertiary)]"
          data-discount-personal-empty
        >
          {{
            t(
              discounts.status === 'unavailable'
                ? 'dashboard.discount.windowsUnavailable'
                : 'dashboard.discount.noPersonal'
            )
          }}
        </p>
      </div>
    </div>

    <EmptyState
      v-else-if="error"
      class="grow !py-8"
      :title="t('dashboard.discount.loadFailed')"
      :hint="error"
      data-discount-error
    />
    <EmptyState
      v-else
      class="grow !py-8"
      :title="t('dashboard.discount.notConnected')"
      :hint="t('dashboard.discount.notConnectedHint')"
      data-discount-not-connected
    />
  </ConsoleCard>
</template>

<style scoped>
.discount-glass-card {
  --d2-glass-panel: var(--glass-surface-regular);
  --d2-glass-row: var(--glass-surface-thin);
  --d2-rim: rgba(56, 55, 43, 0.13);
  --d2-divider: rgba(56, 55, 43, 0.09);
  --d2-etch-highlight: rgba(255, 255, 255, 0.42);
  --d2-bar-global: var(--status-info);
  --d2-bar-personal: var(--accent-text);
  --d2-border-active: var(--accent-text);
  --d2-cap-line: var(--status-danger);
  --d2-track: rgba(56, 55, 43, 0.15);
  --d2-tick-line: var(--surface-solid);
  --d2-idle-fill: rgba(56, 55, 43, 0.44);
  --d2-chamber-sheen:
    radial-gradient(
      150px 85px at 16% 0%,
      rgba(216, 152, 76, 0.11),
      transparent 76%
    ),
    radial-gradient(
      140px 85px at 86% 18%,
      rgba(116, 118, 90, 0.11),
      transparent 76%
    ),
    linear-gradient(
      165deg,
      rgba(255, 255, 255, 0.68) 0%,
      rgba(255, 253, 248, 0.18) 52%,
      rgba(246, 232, 201, 0.2) 100%
    );
  --d2-row-sheen: linear-gradient(
    165deg,
    rgba(255, 255, 255, 0.56) 0%,
    rgba(255, 253, 248, 0.14) 100%
  );
  --d2-selected-sheen: linear-gradient(
    105deg,
    rgba(216, 152, 76, 0.14) 0%,
    rgba(255, 255, 255, 0.38) 48%,
    rgba(216, 152, 76, 0.08) 100%
  );
  --d2-idle-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.62), 0 1px 2px rgba(56, 55, 43, 0.04);
  --d2-selected-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.72),
    inset 0 -1px 0 rgba(192, 139, 87, 0.18),
    0 3px 9px -2px rgba(128, 90, 50, 0.12);
  container-type: inline-size;
  background-image:
    radial-gradient(
      220px 140px at 14% 20%,
      rgba(216, 152, 76, 0.09),
      transparent 74%
    ),
    radial-gradient(
      200px 140px at 88% 78%,
      rgba(127, 164, 99, 0.09),
      transparent 74%
    );
}

html.dark .discount-glass-card {
  --d2-rim: rgba(152, 164, 192, 0.16);
  --d2-divider: rgba(152, 164, 192, 0.11);
  --d2-etch-highlight: rgba(255, 255, 255, 0.05);
  --d2-bar-global: var(--signal);
  --d2-bar-personal: var(--accent);
  --d2-border-active: var(--accent-text);
  --d2-cap-line: var(--load-warning);
  --d2-track: rgba(152, 164, 192, 0.16);
  --d2-tick-line: rgba(196, 204, 220, 0.3);
  --d2-idle-fill: rgba(196, 204, 220, 0.48);
  --d2-chamber-sheen:
    radial-gradient(
      155px 90px at 16% 0%,
      rgba(226, 188, 85, 0.14),
      transparent 76%
    ),
    radial-gradient(
      145px 90px at 86% 18%,
      rgba(106, 140, 184, 0.16),
      transparent 76%
    ),
    linear-gradient(
      165deg,
      rgba(255, 255, 255, 0.085) 0%,
      rgba(255, 255, 255, 0.015) 50%,
      rgba(226, 188, 85, 0.04) 100%
    );
  --d2-row-sheen: linear-gradient(
    165deg,
    rgba(255, 255, 255, 0.065) 0%,
    rgba(255, 255, 255, 0.01) 100%
  );
  --d2-selected-sheen: linear-gradient(
    105deg,
    rgba(226, 188, 85, 0.12) 0%,
    rgba(255, 255, 255, 0.09) 48%,
    rgba(226, 188, 85, 0.04) 100%
  );
  --d2-idle-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.08), 0 2px 5px rgba(0, 4, 16, 0.24);
  --d2-selected-shadow:
    inset 0 1px 0 rgba(255, 255, 255, 0.18),
    inset 0 -1px 0 rgba(226, 188, 85, 0.16),
    0 4px 12px -2px rgba(0, 4, 16, 0.42);
  background-image:
    radial-gradient(
      220px 150px at 14% 20%,
      rgba(226, 188, 85, 0.12),
      transparent 74%
    ),
    radial-gradient(
      200px 150px at 88% 78%,
      rgba(106, 140, 184, 0.13),
      transparent 74%
    );
}

.discount-cap-pill {
  padding: 2px 8px;
  font-size: 10px;
  color: var(--accent-text);
  background-color: var(--accent-soft);
  background-image: var(--d2-row-sheen);
  border: 1px solid var(--d2-border-active);
  border-radius: 9999px;
  box-shadow: var(--glass-specular-edge);
  white-space: nowrap;
}

.discount-config-orb {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  background-color: var(--d2-glass-row);
  background-image: var(--d2-row-sheen);
  border: 1px solid var(--d2-rim);
  border-radius: 9999px;
  box-shadow: var(--d2-idle-shadow);
  transition:
    background-color 160ms ease,
    border-color 160ms ease;
}

[data-discount-configure]:hover .discount-config-orb {
  background-color: var(--state-hover-layer);
  border-color: var(--border-strong);
}

.glass-chamber {
  background-color: var(--d2-glass-panel);
  background-image: var(--d2-chamber-sheen);
  backdrop-filter: blur(var(--glass-blur));
  border: 1px solid var(--d2-rim);
  border-radius: 14px;
  box-shadow: var(--elevation-1), var(--glass-specular-edge);
  overflow: hidden;
}

.chamber-hero {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  align-items: center;
  gap: 12px;
  padding: 9px 11px 8px;
}

.ruler-track {
  position: relative;
  display: grid;
  grid-template-columns: repeat(7, minmax(0, 1fr));
  gap: 2.5px;
  padding: 3px;
  background-color: var(--d2-glass-row);
  border: 1px solid var(--d2-rim);
  border-radius: 6px;
  box-shadow:
    inset 0 1px 3px rgba(56, 55, 43, 0.14),
    0 1px 0 var(--d2-etch-highlight);
}

html.dark .ruler-track {
  box-shadow:
    inset 0 1px 3px rgba(0, 4, 16, 0.42),
    0 1px 0 var(--d2-etch-highlight);
}

.ruler-seg {
  height: 10px;
  border-radius: 2px;
}

.ruler-seg--global {
  background-color: var(--d2-bar-global);
  background-image: linear-gradient(
    180deg,
    rgba(255, 255, 255, 0.24) 0%,
    transparent 60%,
    rgba(0, 0, 0, 0.08) 100%
  );
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.34);
}

.ruler-seg--personal {
  background-color: var(--d2-bar-personal);
  background-image: linear-gradient(
    180deg,
    rgba(255, 255, 255, 0.28) 0%,
    transparent 60%,
    rgba(0, 0, 0, 0.08) 100%
  );
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.38);
}

html.dark .ruler-seg--global {
  filter: drop-shadow(
    0 0 3px color-mix(in srgb, var(--d2-bar-global) 40%, transparent)
  );
}

html.dark .ruler-seg--personal {
  filter: drop-shadow(
    0 0 3px color-mix(in srgb, var(--d2-bar-personal) 40%, transparent)
  );
}

.ruler-seg--overflow {
  background: repeating-linear-gradient(
    -45deg,
    transparent 0 2px,
    var(--border-default) 2px 4px
  );
  border: 1px dashed var(--text-tertiary);
}

html.dark .ruler-seg--overflow {
  background: repeating-linear-gradient(
    -45deg,
    transparent 0 2px,
    rgba(242, 153, 78, 0.24) 2px 4px
  );
  border-color: rgba(242, 153, 78, 0.4);
}

.ruler-seg--empty {
  background-color: var(--d2-track);
}

.cap-marker {
  position: absolute;
  top: -3px;
  bottom: -3px;
  width: 2px;
  background-color: var(--d2-cap-line);
  border-radius: 1px;
  box-shadow: 0 0 0 1px var(--surface-solid);
}

html.dark .cap-marker {
  box-shadow:
    0 0 0 1px rgba(46, 50, 64, 0.88),
    0 0 4px color-mix(in srgb, var(--d2-cap-line) 48%, transparent);
}

.chamber-ledger {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 6px 11px 7px;
  border-top: 1px solid var(--d2-divider);
  background-color: var(--d2-glass-row);
  box-shadow: inset 0 1px 0 var(--d2-etch-highlight);
}

.ledger-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.ruler-swatch {
  width: 7.5px;
  height: 7.5px;
  flex-shrink: 0;
  border-radius: 2px;
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.35);
}

.ruler-swatch--global {
  background-color: var(--d2-bar-global);
}

.ruler-swatch--personal {
  background-color: var(--d2-bar-personal);
}

.window-rows {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.wrow {
  position: relative;
  display: grid;
  grid-template-columns: 52px minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  height: 41px;
  padding: 0 11px;
  border-radius: 11px;
}

.wrow::before {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: inherit;
  box-shadow: var(--d2-idle-shadow);
  pointer-events: none;
}

.wrow--idle {
  background-color: var(--d2-glass-row);
  background-image: var(--d2-row-sheen);
  border: 1.5px solid var(--d2-rim);
}

.wrow--selected {
  background-color: var(--accent-soft);
  background-image: var(--d2-selected-sheen);
  border: 1.5px solid var(--d2-border-active);
}

html.dark .wrow--selected {
  background-color: rgba(226, 188, 85, 0.11);
}

.wrow--selected::before {
  box-shadow: var(--d2-selected-shadow);
}

.wrow-mid {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3.5px;
}

.bar-track {
  position: relative;
  height: 4.5px;
  border-radius: 9999px;
  background-color: var(--d2-track);
  box-shadow: inset 0 1px 1.5px rgba(0, 0, 0, 0.08);
  overflow: hidden;
}

.bar-track::after {
  content: '';
  position: absolute;
  inset: 0;
  background-image: repeating-linear-gradient(
    90deg,
    transparent 0 calc(25% - 1px),
    var(--d2-tick-line) calc(25% - 1px) 25%
  );
  opacity: 0.65;
  pointer-events: none;
}

.bar-fill {
  height: 100%;
  border-radius: 9999px;
  background-color: var(--d2-idle-fill);
  background-image: linear-gradient(
    180deg,
    rgba(255, 255, 255, 0.25) 0%,
    transparent 100%
  );
}

.wrow--selected .bar-fill {
  background-color: var(--d2-border-active);
}

html.dark .wrow--selected .bar-fill {
  filter: drop-shadow(
    0 0 3px color-mix(in srgb, var(--d2-border-active) 36%, transparent)
  );
}

@container (max-width: 340px) {
  .chamber-hero {
    grid-template-columns: 1fr;
    gap: 6px;
  }
  .wrow {
    grid-template-columns: 44px minmax(0, 1fr) auto;
    gap: 6px;
    padding: 0 8px;
  }
}
</style>
