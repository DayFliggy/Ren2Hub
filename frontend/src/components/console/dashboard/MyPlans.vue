<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import { useToast } from '@/composables/useToast'
import { formatDate, formatQuota } from '@/utils/format'

interface CurrentSub {
  plan_id: number
  name: string
  total_quota: number
  remain_quota: number
  expire_time: number
  auto_renew: boolean
}

const { t } = useI18n()
const router = useRouter()
const toast = useToast()

const sub = ref<CurrentSub | null>(null)

onMounted(async () => {
  try {
    sub.value = await api.get<CurrentSub>('/api/subscription/self')
  } catch (error) {
    toast.error(error instanceof ApiError ? error.message : t('common.failed'))
  }
})
</script>

<template>
  <ConsoleCard :title="t('dashboard.myPlans')">
    <template #action>
      <button
        type="button"
        class="text-xs text-[var(--text-tertiary)] transition-colors hover:text-[var(--accent-text)]"
        @click="router.push({ name: 'subscription' })"
      >
        {{ t('common.viewMore') }} ›
      </button>
    </template>

    <div v-if="sub" class="space-y-3">
      <!-- current plan: accent-gradient tile (bank-card pattern from the design) -->
      <div
        class="rounded-xl p-4"
        style="
          background: linear-gradient(
            135deg,
            var(--accent) 0%,
            var(--accent-active) 100%
          );
          color: var(--accent-contrast);
        "
      >
        <div class="flex items-center justify-between">
          <p class="text-sm font-semibold opacity-90">
            {{ t('dashboard.currentPlan') }}
          </p>
          <span
            class="rounded px-1.5 py-0.5 text-[10px] font-bold"
            style="
              background: color-mix(in srgb, currentColor 14%, transparent);
            "
            >PRO</span
          >
        </div>
        <p class="mt-2 text-xl font-bold">{{ sub.name }}</p>
        <p class="mt-1 text-xs opacity-80">
          {{ t('dashboard.expireAt', { date: formatDate(sub.expire_time) }) }}
        </p>
        <div
          class="mt-3 h-1.5 overflow-hidden rounded-full"
          style="background: color-mix(in srgb, currentColor 20%, transparent)"
        >
          <div
            class="h-full rounded-full"
            :style="{
              width: `${Math.round((sub.remain_quota / sub.total_quota) * 100)}%`,
              background: 'color-mix(in srgb, currentColor 78%, transparent)',
            }"
          />
        </div>
        <p class="mt-1.5 text-[11px] opacity-80">
          {{
            t('dashboard.remainOf', { value: formatQuota(sub.remain_quota) })
          }}
        </p>
      </div>

      <button
        type="button"
        class="flex w-full items-center justify-between rounded-xl border border-dashed border-[var(--border-default)] px-4 py-3 text-sm text-[var(--text-secondary)] transition-colors hover:border-[var(--border-strong)] hover:text-[var(--accent-text)]"
        @click="router.push({ name: 'subscription' })"
      >
        {{ t('plans.planList') }}
        <span>→</span>
      </button>
    </div>
  </ConsoleCard>
</template>
