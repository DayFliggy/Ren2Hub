<script setup lang="ts">
import { computed, onScopeDispose, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  publicCatalogApi,
  type RankingPeriod,
  type RankingsSnapshot,
} from '@/api/publicCatalog'
import PublicPage from '@/components/public/PublicPage.vue'
import RankingHistoryChart from '@/components/public/RankingHistoryChart.vue'
import VendorLogo from '@/components/console/models/VendorLogo.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import FilterSelect from '@/components/common/FilterSelect.vue'
import { publicPageMessages } from '@/i18n/publicPages'

const { t, locale } = useI18n({
  useScope: 'local',
  messages: publicPageMessages,
})
const period = ref('week')
const snapshot = ref<RankingsSnapshot | null>(null)
const loading = ref(false)
const error = ref('')
let controller: AbortController | undefined
const periods = computed(() =>
  ['today', 'week', 'month', 'year'].map((value) => ({
    value,
    label: t(`publicPage.${value}`),
  }))
)
const format = (number: number) =>
  new Intl.NumberFormat(locale.value, {
    notation: 'compact',
    maximumFractionDigits: 2,
  }).format(number)
const percent = (number: number) => `${number.toFixed(2)}%`
const growth = (number: number) => `${number > 0 ? '+' : ''}${percent(number)}`
const changes = computed(() => [
  { title: t('publicPage.movers'), rows: snapshot.value?.top_movers ?? [] },
  { title: t('publicPage.droppers'), rows: snapshot.value?.top_droppers ?? [] },
])

async function load() {
  controller?.abort()
  const request = new AbortController()
  controller = request
  loading.value = true
  error.value = ''
  try {
    const result = await publicCatalogApi.rankings(
      period.value as RankingPeriod,
      request.signal
    )
    if (!request.signal.aborted) snapshot.value = result
  } catch (cause) {
    if (!request.signal.aborted)
      error.value =
        cause instanceof Error ? cause.message : t('common.loadFailed')
  } finally {
    if (!request.signal.aborted) loading.value = false
  }
}
watch(period, load, { immediate: true })
onScopeDispose(() => controller?.abort())
</script>

<template>
  <PublicPage :title="t('publicPage.rankings')">
    <template #actions
      ><FilterSelect
        v-model="period"
        :options="periods"
        :label="t('publicPage.period')"
    /></template>
    <div
      v-if="loading"
      role="status"
      :aria-label="t('publicPage.loading')"
      class="space-y-4"
    >
      <SkeletonBlock class="h-80 w-full" /><SkeletonBlock class="h-48 w-full" />
    </div>
    <ErrorBanner v-else-if="error" :message="error" @retry="load" />
    <EmptyState
      v-else-if="!snapshot?.models.length"
      :title="t('publicPage.noActivity')"
    />
    <div v-else class="space-y-10">
      <section>
        <h2 class="mb-5 text-lg font-semibold">{{ t('publicPage.models') }}</h2>
        <div
          class="overflow-x-auto"
          role="region"
          tabindex="0"
          :aria-label="t('publicPage.models')"
        >
          <table class="min-w-[640px]">
            <thead>
              <tr>
                <th>{{ t('publicPage.rank') }}</th>
                <th>{{ t('publicPage.model') }}</th>
                <th>{{ t('publicPage.tokens') }}</th>
                <th>{{ t('publicPage.share') }}</th>
                <th>{{ t('publicPage.growth') }}</th>
                <th>{{ t('publicPage.rankChange') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="model in snapshot.models" :key="model.model_name">
                <td class="font-mono">{{ model.rank }}</td>
                <td class="max-w-80">
                  <div class="flex items-center gap-3">
                    <VendorLogo :vendor="model.vendor" :size="32" />
                    <div class="min-w-0">
                      <RouterLink
                        :to="{
                          name: 'pricing-model',
                          params: { modelId: model.model_name },
                        }"
                        class="break-words font-medium text-[var(--accent-text)]"
                        >{{ model.model_name }}</RouterLink
                      >
                      <p class="text-xs text-[var(--text-tertiary)]">
                        {{ model.vendor }}
                      </p>
                    </div>
                  </div>
                </td>
                <td class="font-mono">{{ format(model.total_tokens) }}</td>
                <td class="font-mono">{{ percent(model.share * 100) }}</td>
                <td class="font-mono">{{ growth(model.growth_pct) }}</td>
                <td class="font-mono">
                  {{
                    model.previous_rank === null
                      ? t('publicPage.new')
                      : model.previous_rank - model.rank
                  }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
      <section v-if="snapshot.modelHistory.length">
        <h2 class="mb-4 text-lg font-semibold">
          {{ t('publicPage.history') }}
        </h2>
        <RankingHistoryChart
          :points="snapshot.modelHistory"
          :title="t('publicPage.history')"
        />
      </section>
      <section>
        <h2 class="mb-5 text-lg font-semibold">
          {{ t('publicPage.vendors') }}
        </h2>
        <div
          class="overflow-x-auto"
          role="region"
          tabindex="0"
          :aria-label="t('publicPage.vendors')"
        >
          <table class="min-w-[560px]">
            <thead>
              <tr>
                <th>{{ t('publicPage.vendor') }}</th>
                <th>{{ t('publicPage.tokens') }}</th>
                <th>{{ t('publicPage.share') }}</th>
                <th>{{ t('publicPage.growth') }}</th>
                <th>{{ t('publicPage.modelCount') }}</th>
                <th>{{ t('publicPage.topModel') }}</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="vendor in snapshot.vendors" :key="vendor.vendor">
                <td>
                  <span class="flex items-center gap-2"
                    ><VendorLogo :vendor="vendor.vendor" :size="28" />{{
                      vendor.vendor
                    }}</span
                  >
                </td>
                <td class="font-mono">{{ format(vendor.total_tokens) }}</td>
                <td class="font-mono">{{ percent(vendor.share * 100) }}</td>
                <td class="font-mono">{{ growth(vendor.growth_pct) }}</td>
                <td class="font-mono">{{ vendor.models_count }}</td>
                <td class="max-w-60 break-words">{{ vendor.top_model }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
      <section v-if="snapshot.vendorHistory.length">
        <h2 class="mb-4 text-lg font-semibold">
          {{ t('publicPage.vendorHistory') }}
        </h2>
        <RankingHistoryChart
          :points="snapshot.vendorHistory"
          :title="t('publicPage.vendorHistory')"
          percentage
        />
      </section>
      <div class="grid gap-8 lg:grid-cols-2">
        <section v-for="change in changes" :key="change.title">
          <h2 class="mb-4 text-lg font-semibold">{{ change.title }}</h2>
          <EmptyState
            v-if="!change.rows.length"
            :title="t('publicPage.empty')"
          />
          <ul v-else class="divide-y divide-[var(--border-subtle)]">
            <li
              v-for="model in change.rows"
              :key="model.model_name"
              class="flex items-center gap-4 py-4"
            >
              <RouterLink
                :to="{
                  name: 'pricing-model',
                  params: { modelId: model.model_name },
                }"
                class="min-w-0 flex-1 break-words text-sm text-[var(--accent-text)]"
                >{{ model.model_name }}</RouterLink
              ><span
                class="font-mono text-sm"
                :aria-label="t('publicPage.rankChange')"
                >{{ model.rank_delta > 0 ? '+' : ''
                }}{{ model.rank_delta }}</span
              ><span
                class="font-mono text-sm"
                :aria-label="t('publicPage.growth')"
                >{{ growth(model.growth_pct) }}</span
              >
            </li>
          </ul>
        </section>
      </div>
    </div>
  </PublicPage>
</template>
