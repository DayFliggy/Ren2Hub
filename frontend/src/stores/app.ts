import { defineStore } from 'pinia'
import { ref } from 'vue'

export const useAppStore = defineStore('app', () => {
  const initialized = ref(false)

  function initialize(): void {
    initialized.value = true
  }

  return { initialized, initialize }
})
