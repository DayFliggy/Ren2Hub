<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import type { Plan } from '@/types/console'
import PageHero from '@/components/console/PageHero.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ConsoleToggle from '@/components/common/ConsoleToggle.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import FormField from '@/components/common/FormField.vue'
import { useToast } from '@/composables/useToast'
import {
  formatCompact,
  formatDate,
  formatMoney,
  formatQuota,
} from '@/utils/format'

interface CurrentSub {
  plan_id: number
  name: string
  total_quota: number
  remain_quota: number
  expire_time: number
  auto_renew: boolean
}

const { t } = useI18n()
const toast = useToast()

const plans = ref<Plan[]>([])
const sub = ref<CurrentSub | null>(null)
const buying = ref<Plan | null>(null)
const method = ref('epay')
const purchasing = ref(false)
const loading = ref(true)
const updatingRenew = ref(false)

/** Remaining-quota bar width, clamped and zero-guarded. */
function usagePercent(s: CurrentSub) {
  if (!s.total_quota) return 0
  return Math.min(
    100,
    Math.max(0, Math.round((s.remain_quota / s.total_quota) * 100))
  )
}

async function loadSub() {
  sub.value = await api.get<CurrentSub>('/api/subscription/self')
}

async function updateAutoRenew(value: boolean) {
  if (!sub.value) return
  updatingRenew.value = true
  const prev = sub.value.auto_renew
  sub.value.auto_renew = value // optimistic
  try {
    await api.put('/api/subscription/self', { auto_renew: value })
  } catch (error) {
    if (sub.value) sub.value.auto_renew = prev // revert on failure
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    updatingRenew.value = false
  }
}

/**
 * Colored tiles pair each gradient with a text color that stays readable in
 * BOTH themes: accent tile follows --accent-contrast, signal tile is always
 * dark enough for --on-colored, support tile is a LIGHT color (sand gold
 * day / lavender night), so it uses --accent-contrast too — dark ink in both
 * themes (~6:1 day, ~7:1 night).
 */
const tileOf: Record<Plan['gradient'], { bg: string; fg: string }> = {
  accent: {
    bg: 'linear-gradient(135deg, var(--accent) 0%, var(--accent-active) 100%)',
    fg: 'var(--accent-contrast)',
  },
  signal: {
    bg: 'linear-gradient(135deg, var(--signal-strong) 0%, var(--signal-deep) 100%)',
    fg: 'var(--on-colored)',
  },
  support: {
    bg: 'linear-gradient(135deg, var(--support) 0%, var(--support-strong) 100%)',
    fg: 'var(--accent-contrast)',
  },
}

const methodOptions = computed(() => [
  { value: 'epay', label: t('wallet.epay') },
  { value: 'stripe', label: t('wallet.stripe') },
  { value: 'creem', label: t('wallet.creem') },
])

async function purchase() {
  if (!buying.value) return
  purchasing.value = true
  try {
    const res = await api.post<{ message: string }>(
      '/api/subscription/purchase',
      {
        plan_id: buying.value.id,
        method: method.value,
      }
    )
    toast.success(res.message || t('plans.purchased'))
    buying.value = null
    await loadSub() // reflect the new current plan
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : String(error))
  } finally {
    purchasing.value = false
  }
}

onMounted(async () => {
  loading.value = true
  try {
    const [planList, self] = await Promise.all([
      api.get<Plan[]>('/api/subscription/plans'),
      api.get<CurrentSub>('/api/subscription/self'),
    ])
    plans.value = planList
    sub.value = self
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : t('common.failed'))
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div>
    <PageHero
      :title="t('plans.title')"
      :crumbs="[$t('plans.breadcrumb.0'), $t('plans.breadcrumb.1')]"
    />

    <!-- loading skeleton -->
    <div v-if="loading" class="space-y-5">
      <div class="h-28 animate-pulse rounded-2xl bg-[var(--surface-muted)]" />
      <div class="grid gap-5 md:grid-cols-3">
        <div
          v-for="i in 3"
          :key="i"
          class="h-72 animate-pulse rounded-2xl bg-[var(--surface-muted)]"
        />
      </div>
    </div>

    <!-- current subscription -->
    <ConsoleCard v-else-if="sub" :title="t('plans.current')" class="mb-5">
      <div class="flex flex-wrap items-center gap-6">
        <div>
          <p class="text-2xl font-bold text-[var(--text-primary)]">
            {{ sub.name }}
          </p>
          <p class="mt-1 text-xs text-[var(--text-tertiary)]">
            {{ t('plans.renewsAt', { date: formatDate(sub.expire_time) }) }}
          </p>
        </div>
        <div class="min-w-52 flex-1">
          <div
            class="mb-1.5 flex justify-between text-xs text-[var(--text-tertiary)]"
          >
            <span>{{ t('plans.usage') }}</span>
            <span
              >{{ formatQuota(sub.remain_quota) }} /
              {{ formatQuota(sub.total_quota) }}</span
            >
          </div>
          <div
            class="h-2 overflow-hidden rounded-full bg-[var(--surface-muted)]"
          >
            <div
              class="h-full rounded-full transition-all"
              style="background: var(--accent)"
              :style="{ width: `${usagePercent(sub)}%` }"
            />
          </div>
        </div>
        <label
          class="flex items-center gap-2 text-sm text-[var(--text-secondary)]"
        >
          <ConsoleToggle
            :model-value="sub.auto_renew"
            :label="t('plans.autoRenew')"
            :disabled="updatingRenew"
            @update:model-value="updateAutoRenew"
          />
          {{ t('plans.autoRenew') }}
        </label>
      </div>
    </ConsoleCard>

    <!-- plan cards: gradient tiles (bank-card pattern) -->
    <p
      v-if="!loading"
      class="mb-3 text-sm font-semibold text-[var(--text-secondary)]"
    >
      {{ t('plans.planList') }}
    </p>
    <div v-if="!loading" class="grid gap-5 md:grid-cols-3">
      <div
        v-for="plan in plans"
        :key="plan.id"
        class="relative flex flex-col rounded-2xl p-5 shadow-[var(--card-shadow)] transition-transform hover:-translate-y-1"
        :style="{
          background: tileOf[plan.gradient].bg,
          color: tileOf[plan.gradient].fg,
        }"
      >
        <span
          v-if="plan.recommended"
          class="absolute right-4 top-4 rounded-md bg-[var(--surface-solid)] px-2 py-0.5 text-xs font-semibold text-[var(--text-primary)] shadow-sm"
        >
          ★ {{ t('plans.recommended') }}
        </span>
        <p class="text-sm font-semibold opacity-90">{{ plan.name }}</p>
        <p class="mt-3">
          <span class="text-3xl font-bold">{{
            formatMoney(plan.price, 0)
          }}</span>
          <span class="text-sm opacity-80">{{ t('plans.perMonth') }}</span>
        </p>
        <p class="mt-1 text-xs opacity-80">
          {{ t('plans.quotaPerMonth', { value: formatCompact(plan.quota) }) }}
        </p>
        <ul class="mt-4 flex-1 space-y-2 text-xs opacity-90">
          <li
            v-for="feature in plan.features"
            :key="feature"
            class="flex items-center gap-2"
          >
            <span
              class="flex h-4 w-4 items-center justify-center rounded-full text-[10px]"
              style="
                background: color-mix(in srgb, currentColor 20%, transparent);
              "
              >✓</span
            >
            {{ feature }}
          </li>
        </ul>
        <button
          type="button"
          class="mt-5 h-11 rounded-xl bg-[var(--surface-solid)] font-semibold text-[var(--text-primary)] transition hover:opacity-90 focus-ring"
          :disabled="sub?.plan_id === plan.id"
          @click="buying = plan"
        >
          {{
            sub?.plan_id === plan.id
              ? `✓ ${t('plans.current')}`
              : t('plans.buy')
          }}
        </button>
      </div>
    </div>

    <!-- purchase modal -->
    <ConsoleModal
      :open="buying !== null"
      :title="t('plans.purchaseTitle')"
      size="sm"
      @close="buying = null"
    >
      <div v-if="buying" class="space-y-4">
        <div
          class="rounded-xl p-4"
          :style="{
            background: tileOf[buying.gradient].bg,
            color: tileOf[buying.gradient].fg,
          }"
        >
          <div class="flex items-center justify-between">
            <p class="font-bold">{{ buying.name }}</p>
            <p class="text-lg font-bold">
              {{ formatMoney(buying.price, 0)
              }}<span class="text-xs opacity-80">{{
                t('plans.perMonth')
              }}</span>
            </p>
          </div>
          <p class="mt-1 text-xs opacity-80">
            {{
              t('plans.quotaPerMonth', { value: formatCompact(buying.quota) })
            }}
            · {{ t('plans.days', { n: buying.duration_days }) }}
          </p>
        </div>
        <FormField :label="t('plans.purchaseMethod')">
          <FilterSelect
            v-model="method"
            :options="methodOptions"
            :label="t('plans.purchaseMethod')"
          />
        </FormField>
        <p class="text-xs leading-relaxed text-[var(--text-tertiary)]">
          {{ t('plans.purchaseHint') }}
        </p>
      </div>
      <template #footer>
        <ConsoleButton size="lg" block :loading="purchasing" @click="purchase">
          {{ t('common.confirm') }}
        </ConsoleButton>
      </template>
    </ConsoleModal>
  </div>
</template>
