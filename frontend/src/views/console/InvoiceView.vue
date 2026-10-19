<script setup lang="ts">
import { reactive } from 'vue'
import { useI18n } from 'vue-i18n'

import PageHero from '@/components/console/PageHero.vue'
import ConsoleButton from '@/components/common/ConsoleButton.vue'
import ConsoleCard from '@/components/common/ConsoleCard.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import FormField from '@/components/common/FormField.vue'
import TextInput from '@/components/common/TextInput.vue'
import { invoiceMessages } from '@/i18n/invoice'

const { t } = useI18n({ useScope: 'local', messages: invoiceMessages })

// The draft stays on this page. Invoice submission and records are unavailable.
const form = reactive({
  title: '',
  tax_id: '',
  amount: '',
  email: '',
  note: '',
})
</script>

<template>
  <div>
    <PageHero
      :title="t('invoice.title')"
      :crumbs="[t('invoice.breadcrumb.0'), t('invoice.breadcrumb.1')]"
    >
      <p class="mt-1 text-sm text-[var(--text-tertiary)]">
        {{ t('invoice.bannerDesc') }}
      </p>
      <template #actions>
        <div
          id="invoice-contact-owner-note"
          role="note"
          class="inline-flex h-10 items-center gap-2 rounded-xl border border-[var(--status-info)] bg-[var(--status-info-soft)] px-4 text-sm font-semibold text-[var(--status-info-text)]"
        >
          <svg
            width="17"
            height="17"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path
              d="M21 15a4 4 0 0 1-4 4H8l-5 3V7a4 4 0 0 1 4-4h10a4 4 0 0 1 4 4z"
            />
          </svg>
          {{ t('invoice.contactOwner') }}
        </div>
      </template>
    </PageHero>

    <!-- ── Main two-column layout ── -->
    <div class="grid gap-6 lg:grid-cols-5">
      <!-- ── Left: application form (3 / 5) ── -->
      <ConsoleCard :title="t('invoice.formTitle')" class="lg:col-span-3">
        <form class="sibling-space-y-5" @submit.prevent>
          <!-- 发票抬头 * -->
          <FormField :label="`${t('invoice.titleLabel')} *`">
            <TextInput
              v-model="form.title"
              :placeholder="t('invoice.titlePlaceholder')"
            >
              <template #icon>
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  aria-hidden="true"
                >
                  <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
                  <circle cx="12" cy="7" r="4" />
                </svg>
              </template>
            </TextInput>
          </FormField>

          <!-- 税号 + 开票金额 * (two columns) -->
          <div class="grid gap-4 sm:grid-cols-2">
            <FormField :label="t('invoice.taxIdLabel')">
              <TextInput
                v-model="form.tax_id"
                :placeholder="t('invoice.taxIdPlaceholder')"
              >
                <template #icon>
                  <svg
                    width="16"
                    height="16"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    aria-hidden="true"
                  >
                    <path
                      d="M9 5H7a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V7a2 2 0 0 0-2-2h-2"
                    />
                    <rect x="9" y="3" width="6" height="4" rx="1" />
                  </svg>
                </template>
              </TextInput>
            </FormField>
            <FormField :label="`${t('invoice.amountLabel')} *`">
              <TextInput
                v-model="form.amount"
                type="number"
                step="0.01"
                :placeholder="t('invoice.amountPlaceholder')"
              >
                <template #icon>
                  <svg
                    width="16"
                    height="16"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    stroke-width="2"
                    stroke-linecap="round"
                    stroke-linejoin="round"
                    aria-hidden="true"
                  >
                    <circle cx="12" cy="12" r="10" />
                    <path d="M16 8h-6a2 2 0 1 0 0 4h4a2 2 0 1 1 0 4H8" />
                    <line x1="12" y1="6" x2="12" y2="8" />
                    <line x1="12" y1="16" x2="12" y2="18" />
                  </svg>
                </template>
              </TextInput>
              <p class="mt-1.5 text-xs text-[var(--text-tertiary)]">
                {{ t('invoice.amountHint') }}
              </p>
            </FormField>
          </div>

          <!-- 接收邮箱 -->
          <FormField :label="t('invoice.emailLabel')">
            <TextInput
              v-model="form.email"
              type="email"
              :placeholder="t('invoice.emailPlaceholder')"
            >
              <template #icon>
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="2"
                  stroke-linecap="round"
                  stroke-linejoin="round"
                  aria-hidden="true"
                >
                  <rect x="2" y="4" width="20" height="16" rx="2" />
                  <path d="m22 7-8.97 5.7a1.94 1.94 0 0 1-2.06 0L2 7" />
                </svg>
              </template>
            </TextInput>
          </FormField>

          <!-- 备注 -->
          <FormField :label="t('invoice.noteLabel')">
            <textarea
              v-model="form.note"
              rows="3"
              :placeholder="t('invoice.notePlaceholder')"
              class="w-full resize-y rounded-xl border border-[var(--border-subtle)] bg-[var(--surface-solid)] px-4 py-3 text-sm text-[var(--text-primary)] placeholder:text-[var(--text-tertiary)] outline-hidden transition focus:border-[var(--border-strong)] focus-ring"
            />
          </FormField>

          <!-- Keep the historical form without claiming an available service. -->
          <div class="sibling-space-y-3">
            <p
              id="invoice-submit-note"
              class="flex items-start gap-2 text-xs leading-5 text-[var(--status-info-text)]"
            >
              <svg
                width="14"
                height="14"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
                class="mt-0.5 shrink-0"
              >
                <circle cx="12" cy="12" r="10" />
                <path d="M12 16v-4M12 8h.01" />
              </svg>
              {{ t('invoice.submitOfflineNote') }}
            </p>
            <ConsoleButton
              type="submit"
              size="lg"
              block
              disabled
              aria-describedby="invoice-submit-note"
            >
              <svg
                width="18"
                height="18"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                stroke-width="2"
                stroke-linecap="round"
                stroke-linejoin="round"
                aria-hidden="true"
                class="shrink-0"
              >
                <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14" />
                <path d="m9 11 3 3L22 4" />
              </svg>
              {{ t('invoice.submit') }}
            </ConsoleButton>
          </div>
        </form>
      </ConsoleCard>

      <!-- ── Right: invoice record list (2 / 5) ── -->
      <div class="lg:col-span-2">
        <ConsoleCard :padded="false">
          <template #action>
            <span class="text-sm font-semibold text-[var(--text-primary)]">
              {{ t('invoice.listTitle') }}
            </span>
          </template>

          <EmptyState
            :title="t('invoice.recordsUnavailable')"
            :hint="t('invoice.recordsUnavailableHint')"
            illustration="empty-invoice"
            role="status"
          />
        </ConsoleCard>
      </div>
    </div>
  </div>
</template>
