<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'

import InviteAffiliateCard from '@/components/console/invite/InviteAffiliateCard.vue'
import InviteMonthChart from '@/components/console/invite/InviteMonthChart.vue'
import InviteRewardCard from '@/components/console/invite/InviteRewardCard.vue'
import PageHero from '@/components/console/PageHero.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ConsoleModal from '@/components/common/ConsoleModal.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import SectionHeading from '@/components/common/SectionHeading.vue'
import SkeletonBlock from '@/components/common/SkeletonBlock.vue'
import SegmentedToggle from '@/components/common/SegmentedToggle.vue'
import StatusChip from '@/components/common/StatusChip.vue'
import { useInvite } from '@/composables/useInvite'
import { formatDate, formatQuota } from '@/utils/format'

const { t } = useI18n()
const {
  info,
  loadError,
  inviteLink,
  invitees,
  inviteesPage,
  inviteesTotal,
  inviteesPageCount,
  inviteesOrder,
  inviteesLoading,
  inviteesError,
  loadInvitees,
  sortInvitees,
  transferOpen,
  canTransfer,
  transferring,
  readOnly,
  load,
  copyCode,
  copyLink,
  shareX,
  shareTelegram,
  shareEmail,
  transfer,
} = useInvite()

const howSteps = [1, 2, 3, 4]
const faqs = [1, 2, 3, 4]

onMounted(() => Promise.all([load(), loadInvitees()]))
</script>

<template>
  <div>
    <PageHero
      :title="t('invite.title')"
      :crumbs="[$t('invite.breadcrumb.0'), $t('invite.breadcrumb.1')]"
    >
      <template #actions>
        <div class="text-right">
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ t('invite.heroTotal') }}
          </p>
          <p
            class="display-number text-3xl tracking-tight text-[var(--text-primary)]"
          >
            {{ info ? formatQuota(info.reward_total) : '--' }}
          </p>
        </div>
      </template>
    </PageHero>

    <ErrorBanner
      v-if="loadError"
      class="mt-5"
      :message="loadError"
      @retry="load()"
    />

    <!-- stat cards -->
    <div class="mb-5 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <ConsoleCard>
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('invite.invitedCount') }}
        </p>
        <p class="display-number mt-1.5 text-2xl text-[var(--text-primary)]">
          {{ info ? t('invite.people', { count: info.invited }) : '--' }}
        </p>
        <p class="mt-1 text-xs text-[var(--text-tertiary)]">
          {{ t('invite.invitedCountHint') }}
        </p>
      </ConsoleCard>

      <ConsoleCard>
        <div class="flex items-start justify-between gap-2">
          <div class="min-w-0">
            <p class="text-xs text-[var(--text-tertiary)]">
              {{ t('invite.transferable') }}
            </p>
            <p class="display-number mt-1.5 text-2xl text-[var(--accent-text)]">
              {{ info ? formatQuota(info.transferable) : '--' }}
            </p>
          </div>
          <ConsoleButton
            size="sm"
            :disabled="!canTransfer"
            @click="transferOpen = true"
          >
            {{ t('invite.transfer') }}
          </ConsoleButton>
        </div>
        <p class="mt-1 text-xs text-[var(--text-tertiary)]">
          {{ t('invite.transferableHint') }}
        </p>
      </ConsoleCard>

      <ConsoleCard>
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('invite.rebateRate') }}
        </p>
        <p class="display-number mt-1.5 text-2xl text-[var(--accent-text)]">
          {{ info ? `${info.effective_rate_percent}%` : '--' }}
        </p>
        <p class="mt-1 text-xs text-[var(--text-tertiary)]">
          {{ t('invite.rebateRateHint') }}
        </p>
      </ConsoleCard>

      <ConsoleCard>
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('invite.frozenReward') }}
        </p>
        <p class="display-number mt-1.5 text-2xl text-[var(--text-primary)]">
          {{ info ? formatQuota(info.frozen_reward) : '--' }}
        </p>
        <p class="mt-1 text-xs text-[var(--text-tertiary)]">
          {{
            t('invite.frozenRewardHint', {
              hours: info?.policy.freeze_hours ?? 0,
            })
          }}
        </p>
      </ConsoleCard>
    </div>

    <!-- affiliate + invitees -->
    <div class="grid gap-5 lg:grid-cols-[1fr_22.5rem]">
      <InviteAffiliateCard
        :code="info?.code ?? ''"
        :invite-link="inviteLink"
        :rate-percent="info?.effective_rate_percent ?? 0"
        @copy-code="copyCode"
        @copy-link="copyLink"
        @share-x="shareX"
        @share-telegram="shareTelegram"
        @share-email="shareEmail"
      />

      <!-- Wrapper contributes no intrinsic height on desktop (its child is
           absolute), so the affiliate card alone drives the row height — no
           blank space there. The invitee card fills that height and its list
           scrolls. On mobile (no lg:) it's static and fully expands. -->
      <div class="lg:relative">
        <section
          class="flex flex-col overflow-hidden rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] lg:absolute lg:inset-0"
        >
          <div
            class="flex shrink-0 items-center justify-between gap-3 px-5 pt-4"
          >
            <h3
              class="flex items-center gap-2 text-sm font-semibold text-[var(--text-primary)]"
            >
              {{ t('invite.invitees') }}
              <StatusChip tone="neutral">{{
                t('invite.people', { count: inviteesTotal })
              }}</StatusChip>
            </h3>
            <SegmentedToggle
              :model-value="inviteesOrder"
              size="sm"
              :options="[
                { value: 'desc', label: t('invite.sortRecent') },
                { value: 'asc', label: t('invite.sortEarly') },
              ]"
              :label="t('invite.invitees')"
              @update:model-value="sortInvitees"
            />
          </div>

          <ErrorBanner
            v-if="inviteesError"
            class="mx-5 mt-3"
            :message="inviteesError"
            @retry="loadInvitees()"
          />
          <p
            v-if="inviteesLoading"
            class="px-5 py-3 text-xs text-[var(--text-tertiary)]"
            role="status"
          >
            {{ t('common.loading') }}
          </p>
          <ul
            class="subtle-scroll mt-2 sibling-divide-y sibling-divide-[var(--border-subtle)] px-5 lg:min-h-0 lg:flex-1 lg:overflow-y-auto"
          >
            <li
              v-for="(invitee, index) in invitees"
              :key="`${inviteesPage}-${index}`"
              class="flex items-center justify-between gap-3 py-3"
            >
              <div class="flex min-w-0 items-center gap-2.5">
                <span
                  class="inline-flex shrink-0 items-center rounded-md bg-[var(--surface-muted)] px-2.5 py-0.5 font-mono text-xs tracking-wide text-[var(--text-secondary)]"
                >
                  {{ invitee.masked_name }}
                </span>
              </div>
              <div class="text-right">
                <div class="mb-1 flex items-center justify-end gap-2">
                  <span
                    class="text-xs font-medium text-[var(--text-secondary)]"
                  >
                    {{ formatQuota(invitee.reward_quota) }}
                  </span>
                </div>
                <p class="text-xs text-[var(--text-tertiary)]">
                  {{ formatDate(invitee.invited_at) }}
                </p>
              </div>
            </li>
            <li
              v-if="!inviteesLoading && !inviteesError && invitees.length === 0"
              class="py-10 text-center text-sm text-[var(--text-tertiary)]"
            >
              {{ t('common.none') }}
            </li>
          </ul>
          <div
            v-if="inviteesPageCount > 1"
            class="flex shrink-0 items-center justify-between gap-2 px-5 py-2"
          >
            <ConsoleButton
              size="sm"
              variant="ghost"
              :disabled="inviteesLoading || inviteesPage <= 1"
              @click="loadInvitees(inviteesPage - 1)"
              >{{ t('common.prevPage') }}</ConsoleButton
            >
            <span
              class="text-xs text-[var(--text-tertiary)]"
              aria-live="polite"
              >{{
                t('common.pageProgress', {
                  page: inviteesPage,
                  count: inviteesPageCount,
                })
              }}</span
            >
            <ConsoleButton
              size="sm"
              variant="ghost"
              :disabled="inviteesLoading || inviteesPage >= inviteesPageCount"
              @click="loadInvitees(inviteesPage + 1)"
              >{{ t('common.nextPage') }}</ConsoleButton
            >
          </div>

          <p
            class="shrink-0 border-t border-[var(--border-subtle)] px-5 py-3 text-xs text-[var(--text-tertiary)]"
          >
            {{ t('invite.inviteesNote') }}
          </p>
        </section>
      </div>
    </div>

    <!-- chart + reward balance -->
    <div class="mt-5 grid gap-5 lg:grid-cols-[1fr_22.5rem]">
      <InviteMonthChart v-if="info" :series="info.monthly_series" />
      <SkeletonBlock v-else class="h-72" />

      <InviteRewardCard
        v-if="info"
        :transferable="info.transferable"
        :frozen="info.frozen_reward"
        :reward-total="info.reward_total"
        :invited="info.invited"
        :readonly="readOnly"
        @transfer="transferOpen = true"
      />
      <SkeletonBlock v-else class="h-72" />
    </div>

    <!-- how it works -->
    <section class="mt-8">
      <SectionHeading class="mb-4">
        {{ t('invite.howTitle') }}
      </SectionHeading>
      <div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <div
          v-for="n in howSteps"
          :key="n"
          class="rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] p-5"
        >
          <span class="display-number text-2xl text-[var(--accent-text)]"
            >0{{ n }}</span
          >
          <p class="mt-2 text-sm font-semibold text-[var(--text-primary)]">
            {{ t(`invite.howStep${n}Title`) }}
          </p>
          <p class="mt-1.5 text-xs leading-relaxed text-[var(--text-tertiary)]">
            {{
              t(`invite.howStep${n}Desc`, {
                reward: `${info?.effective_rate_percent ?? 0}%`,
              })
            }}
          </p>
        </div>
      </div>
    </section>

    <!-- faq -->
    <section class="mt-8">
      <h3 class="mb-4 text-lg font-bold text-[var(--text-primary)]">
        {{ t('invite.faqTitle') }}
      </h3>
      <div class="sibling-space-y-3">
        <details
          v-for="n in faqs"
          :key="n"
          class="group rounded-2xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-5"
        >
          <summary
            class="flex cursor-pointer list-none items-center justify-between gap-3 py-4 text-sm font-medium text-[var(--text-primary)]"
          >
            {{ t(`invite.faqQ${n}`) }}
            <svg
              class="shrink-0 text-[var(--text-tertiary)] transition-transform group-open:rotate-180"
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              stroke-width="2"
              aria-hidden="true"
            >
              <path d="m6 9 6 6 6-6" />
            </svg>
          </summary>
          <p class="pb-4 text-sm leading-relaxed text-[var(--text-tertiary)]">
            {{
              t(`invite.faqA${n}`, {
                reward: `${info?.effective_rate_percent ?? 0}%`,
              })
            }}
          </p>
        </details>
      </div>
    </section>

    <!-- transfer modal -->
    <ConsoleModal
      :open="transferOpen"
      :title="t('invite.transferTitle')"
      :subtitle="t('invite.transferHint')"
      size="sm"
      @close="transferOpen = false"
    >
      <div class="sibling-space-y-3">
        <p class="text-xs text-[var(--text-tertiary)]">
          {{ t('invite.transferable') }}:
          {{ formatQuota(info?.transferable ?? 0) }}
        </p>
      </div>
      <template #footer>
        <ConsoleButton
          size="lg"
          block
          :loading="transferring"
          :disabled="!canTransfer"
          @click="transfer"
        >
          {{ t('common.confirm') }}
        </ConsoleButton>
      </template>
    </ConsoleModal>
  </div>
</template>
