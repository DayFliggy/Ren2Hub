import { ref } from 'vue'
import { useI18n } from 'vue-i18n'

import { api } from '@/api/console'
import { ApiError } from '@/api/types'
import type {
  FarmState,
  FarmPlot,
  RanchAnimal,
  FishingState,
  FarmPet,
  MineState,
  LeaderEntry,
  RebateTier,
  RebateState,
  LeaderPeriod,
} from '@/types/farm'
import { useToast } from '@/composables/useToast'

/**
 * Farm activity state + actions. Owns all loading and mutation so
 * FarmView.vue stays focused on layout composition.
 */
export function useFarm() {
  const { t } = useI18n()
  const toast = useToast()

  const loading = ref(true)
  const acting = ref(false)

  const farmState = ref<FarmState | null>(null)
  const plots = ref<FarmPlot[]>([])
  const animals = ref<RanchAnimal[]>([])
  const fishing = ref<FishingState | null>(null)
  const pet = ref<FarmPet | null>(null)
  const mine = ref<MineState | null>(null)

  const leaderPeriod = ref<LeaderPeriod>('week')
  const leaderLoading = ref(false)
  const leaderEntries = ref<LeaderEntry[]>([])

  const rebateTiers = ref<RebateTier[]>([])
  const rebateState = ref<RebateState | null>(null)
  const rebateLoading = ref(false)

  async function load() {
    loading.value = true
    try {
      const data = await api.get<{
        state: FarmState
        plots: FarmPlot[]
        animals: RanchAnimal[]
        fishing: FishingState
        pet: FarmPet
        mine: MineState
      }>('/api/farm/self')
      farmState.value = data.state
      plots.value = data.plots
      animals.value = data.animals
      fishing.value = data.fishing
      pet.value = data.pet
      mine.value = data.mine
    } catch (error) {
      toast.error(
        error instanceof ApiError ? error.message : t('common.failed')
      )
    } finally {
      loading.value = false
    }
  }

  async function loadLeader(period: LeaderPeriod = leaderPeriod.value) {
    leaderPeriod.value = period
    leaderLoading.value = true
    try {
      const data = await api.get<{
        entries: LeaderEntry[]
        period: LeaderPeriod
      }>('/api/farm/leader', { period })
      leaderEntries.value = data.entries
    } catch (error) {
      toast.error(
        error instanceof ApiError ? error.message : t('common.failed')
      )
    } finally {
      leaderLoading.value = false
    }
  }

  async function loadRebate() {
    rebateLoading.value = true
    try {
      const data = await api.get<{ tiers: RebateTier[]; state: RebateState }>(
        '/api/farm/rebate'
      )
      rebateTiers.value = data.tiers
      rebateState.value = data.state
    } catch (error) {
      toast.error(
        error instanceof ApiError ? error.message : t('common.failed')
      )
    } finally {
      rebateLoading.value = false
    }
  }

  async function harvest(plotId: number) {
    acting.value = true
    try {
      const res = await api.post<{ coins: number; gained: number }>(
        `/api/farm/harvest/${plotId}`
      )
      if (farmState.value) farmState.value.coins = res.coins
      const plot = plots.value.find((p) => p.id === plotId)
      if (plot) {
        plot.stage = 'empty'
        plot.seed = null
        plot.planted_at = null
        plot.harvest_at = null
        plot.yield_quota = 0
      }
      toast.success(
        t('farm.plot.harvested') + ` +${(res.gained / 500000).toFixed(4)}$`
      )
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : String(e))
    } finally {
      acting.value = false
    }
  }

  async function feedAnimal(animalId: number) {
    acting.value = true
    try {
      const res = await api.post<{ animal: RanchAnimal }>(
        `/api/farm/feed/animal/${animalId}`
      )
      const idx = animals.value.findIndex((a) => a.id === animalId)
      if (idx >= 0) animals.value[idx] = res.animal
      toast.success(t('farm.ranch.fed'))
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : String(e))
    } finally {
      acting.value = false
    }
  }

  async function collectAnimal(animalId: number) {
    acting.value = true
    try {
      const res = await api.post<{ coins: number; gained: number }>(
        `/api/farm/collect/animal/${animalId}`
      )
      if (farmState.value) farmState.value.coins = res.coins
      const animal = animals.value.find((a) => a.id === animalId)
      if (animal) animal.yield_ready = false
      toast.success(
        t('farm.ranch.collect') + ` +${(res.gained / 500000).toFixed(4)}$`
      )
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : String(e))
    } finally {
      acting.value = false
    }
  }

  async function goFishing() {
    acting.value = true
    try {
      const res = await api.post<{
        catch: FishingState['last_catch']
        daily_left: number
      }>('/api/farm/fish')
      if (fishing.value) {
        fishing.value.daily_left = res.daily_left
        fishing.value.last_catch = res.catch
      }
      if (res.catch) {
        toast.success(
          `${res.catch.emoji} ${res.catch.name} — ${res.catch.rarity}`
        )
      }
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : String(e))
    } finally {
      acting.value = false
    }
  }

  async function feedPet() {
    acting.value = true
    try {
      const res = await api.post<{ pet: FarmPet }>('/api/farm/feed/pet')
      pet.value = res.pet
      toast.success(t('farm.pet.fed'))
    } catch (e) {
      toast.error(e instanceof ApiError ? e.message : String(e))
    } finally {
      acting.value = false
    }
  }

  return {
    loading,
    acting,
    farmState,
    plots,
    animals,
    fishing,
    pet,
    mine,
    leaderPeriod,
    leaderLoading,
    leaderEntries,
    rebateTiers,
    rebateState,
    rebateLoading,
    load,
    loadLeader,
    loadRebate,
    harvest,
    feedAnimal,
    collectAnimal,
    goFishing,
    feedPet,
  }
}
