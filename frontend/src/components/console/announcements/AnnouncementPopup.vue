<script setup lang="ts">
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'

import { useToast } from '@/composables/useToast'
import { useAnnouncementStore } from '@/stores/announcements'

import AnnouncementDetailModal from './AnnouncementDetailModal.vue'

const { t } = useI18n()
const toast = useToast()
const store = useAnnouncementStore()
const { currentPopup } = storeToRefs(store)

async function handleDismiss() {
  const ok = await store.dismissPopup()
  if (!ok && currentPopup.value !== null) {
    toast.error(t('announcements.markReadFailed'))
  }
}
</script>

<template>
  <AnnouncementDetailModal
    :open="currentPopup !== null"
    :announcement="currentPopup"
    popup
    :z-index="95"
    @close="handleDismiss"
  />
</template>
