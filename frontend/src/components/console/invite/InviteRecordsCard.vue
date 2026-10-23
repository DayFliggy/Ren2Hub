<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { AffiliateRecord } from '@/api/affiliate'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import ErrorBanner from '@/components/common/ErrorBanner.vue'
import TablePagination from '@/components/common/TablePagination.vue'
import { formatDate, formatQuota } from '@/utils/format'

defineProps<{
  records: AffiliateRecord[]
  page: number
  total: number
  loading: boolean
  error: string
}>()
const emit = defineEmits<{ load: [page: number] }>()
const { t, te } = useI18n()

function credited(row: AffiliateRecord): string {
  // Legacy accrual rows predate both split fields and were credited in full.
  return row.credited_quota === '0' && row.debt_offset_quota === '0'
    ? row.reward_quota
    : row.credited_quota
}

function skipReason(reason: string): string {
  const key = `invite.skipReasons.${reason}`
  return te(key) ? t(key) : t('invite.skipped')
}
</script>

<template>
  <ConsoleCard :title="t('invite.records')">
    <ErrorBanner v-if="error" :message="error" @retry="emit('load', page)" />
    <p
      v-else-if="loading"
      role="status"
      class="py-5 text-sm text-[var(--text-tertiary)]"
    >
      {{ t('common.loading') }}
    </p>
    <ul
      v-else-if="records.length"
      class="divide-y divide-[var(--border-subtle)]"
    >
      <li
        v-for="row in records"
        :key="row.id"
        :data-record-kind="row.kind"
        class="flex flex-wrap items-start justify-between gap-3 py-3"
      >
        <div class="space-y-1 text-sm">
          <p class="font-medium text-[var(--text-primary)]">
            {{ t(`invite.recordKinds.${row.kind}`) }}
          </p>
          <p class="text-xs text-[var(--text-tertiary)]">
            {{ formatDate(row.created_at)
            }}<span v-if="row.invitee_id">
              · {{ t('invite.recordInvitee', { id: row.invitee_id }) }}</span
            >
          </p>
          <p v-if="row.skip_reason" class="text-xs text-[var(--text-tertiary)]">
            {{ skipReason(row.skip_reason) }}
          </p>
        </div>
        <div class="space-y-1 text-right text-xs text-[var(--text-secondary)]">
          <p
            class="font-mono text-sm font-medium"
            :class="
              row.kind === 'reversal'
                ? 'text-[var(--status-danger-text)]'
                : 'text-[var(--text-primary)]'
            "
          >
            {{ row.kind === 'reversal' ? '−' : ''
            }}{{ formatQuota(row.reward_quota) }}
          </p>
          <template v-if="row.kind === 'accrue' && !row.skip_reason">
            <p>
              {{
                t('invite.recordCredited', {
                  amount: formatQuota(credited(row)),
                })
              }}
            </p>
            <p v-if="BigInt(row.debt_offset_quota) > 0n">
              {{
                t('invite.recordOffset', {
                  amount: formatQuota(row.debt_offset_quota),
                })
              }}
            </p>
          </template>
          <template v-if="row.kind === 'reversal'">
            <p>
              {{
                t('invite.recordRevoked', {
                  amount: formatQuota(row.credited_quota),
                })
              }}
            </p>
            <p v-if="BigInt(row.debt_added_quota) > 0n">
              {{
                t('invite.recordDebtAdded', {
                  amount: formatQuota(row.debt_added_quota),
                })
              }}
            </p>
          </template>
        </div>
      </li>
    </ul>
    <p v-else class="py-5 text-sm text-[var(--text-tertiary)]">
      {{ t('common.none') }}
    </p>
    <TablePagination
      v-if="!error && !loading && total > 0"
      :page="page"
      :page-size="20"
      :page-sizes="[20]"
      :total="total"
      @update:page="emit('load', $event)"
    />
  </ConsoleCard>
</template>
