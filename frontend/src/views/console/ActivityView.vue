<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PageHero from '@/components/console/PageHero.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import TurnstileWidget from '@/components/auth/TurnstileWidget.vue'
import { useActivity } from '@/composables/useActivity'
import { useAppStore } from '@/stores/app'
import { formatQuota } from '@/utils/format'
import { nativeAccountMessages } from '@/i18n/nativeAccount'
const { t } = useI18n({ messages: nativeAccountMessages }),
  app = useAppStore()
const { month, status, loading, claiming, loadError, load, checkin } =
  useActivity()
const turnstile = ref(''),
  widget = ref<InstanceType<typeof TurnstileWidget> | null>(null)
onMounted(load)
watch(month, load)
async function submit() {
  await checkin(turnstile.value)
  turnstile.value = ''
  widget.value?.reset()
}
</script>
<template>
  <div>
    <PageHero
      :title="t('nativeAccount.checkin')"
      :crumbs="[t('nativeAccount.checkin')]"
    />
    <ErrorBanner v-if="loadError" :message="loadError" />
    <ConsoleCard :title="t('nativeAccount.checkin')"
      ><p>{{ t('nativeAccount.checkinHint') }}</p>
      <label class="my-4 block"
        >{{ t('nativeAccount.month') }}
        <input v-model="month" type="month" class="console-input"
      /></label>
      <div v-if="status" class="space-y-4">
        <p>
          {{ t('nativeAccount.rewardRange') }}:
          {{ formatQuota(status.min_quota) }} –
          {{ formatQuota(status.max_quota) }}
        </p>
        <p>
          {{ t('nativeAccount.days') }}: {{ status.stats.total_checkins }} ·
          {{ t('nativeAccount.rewards') }}:
          {{ formatQuota(status.stats.total_quota) }}
        </p>
        <TurnstileWidget
          v-if="app.turnstileEnabled"
          ref="widget"
          :site-key="app.turnstileSiteKey"
          @verified="turnstile = $event"
        />
        <ConsoleButton
          :disabled="
            !status.enabled ||
            loading ||
            claiming ||
            status.stats.checked_in_today ||
            (app.turnstileEnabled && !turnstile)
          "
          @click="submit"
          >{{
            status.stats.checked_in_today
              ? t('nativeAccount.checked')
              : t('nativeAccount.checkinNow')
          }}</ConsoleButton
        >
        <table class="w-full text-left">
          <thead>
            <tr>
              <th>{{ t('nativeAccount.date') }}</th>
              <th>{{ t('nativeAccount.quota') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="record in status.stats.records"
              :key="record.checkin_date"
            >
              <td class="py-2">{{ record.checkin_date }}</td>
              <td>{{ formatQuota(record.quota_awarded) }}</td>
            </tr>
          </tbody>
        </table>
      </div></ConsoleCard
    >
  </div>
</template>
