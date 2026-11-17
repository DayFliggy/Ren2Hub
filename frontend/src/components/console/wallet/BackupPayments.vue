<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { ExternalLink } from 'lucide-vue-next'

import type { BackupPaymentLink } from '@/api/topup'
import PaymentMethodIcon from './PaymentMethodIcon.vue'
import { safeExternalUrl } from '@/utils/safeUrl'

const props = defineProps<{ links: BackupPaymentLink[] }>()
const { t } = useI18n()

// Administrators choose these sites, so each link's own origin is trusted;
// anything that is not an http(s) URL is dropped.
const items = computed(() =>
  props.links.flatMap((link, index) => {
    const href = safeExternalUrl(link.url, [link.url])
    return href
      ? [
          {
            key: `${index}-${href}`,
            href,
            icon: link.icon,
            name: link.name || t('wallet.externalTitle'),
          },
        ]
      : []
  })
)
</script>

<template>
  <section v-if="items.length" aria-labelledby="backup-payments-title">
    <h3
      id="backup-payments-title"
      class="text-xs font-semibold text-[var(--text-secondary)]"
    >
      {{ t('wallet.backupTitle') }}
    </h3>
    <p class="mt-0.5 text-xs text-[var(--text-tertiary)]">
      {{ t('wallet.backupHint') }}
    </p>
    <ul class="mt-3 grid gap-2" :class="{ 'grid-cols-2': items.length > 4 }">
      <li v-for="item in items" :key="item.key" class="min-w-0">
        <a
          :href="item.href"
          target="_blank"
          rel="noopener noreferrer"
          class="focus-ring flex h-11 items-center justify-between gap-2 rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-muted)] px-3 text-sm font-medium text-[var(--text-primary)] transition-colors hover:border-[var(--border-default)] hover:bg-[var(--surface-hover)]"
        >
          <span class="flex min-w-0 items-center gap-2">
            <PaymentMethodIcon
              v-if="item.icon"
              :icon="item.icon"
              fallback="link"
            />
            <span class="min-w-0 truncate">{{ item.name }}</span>
          </span>
          <ExternalLink
            class="size-3.5 shrink-0 text-[var(--text-tertiary)]"
            aria-hidden="true"
          />
          <span class="sr-only">{{ t('wallet.externalNewTab') }}</span>
        </a>
      </li>
    </ul>
  </section>
</template>
