<template>
  <div class="overflow-visible px-1 py-[var(--space-2)]">
    <div
      class="mb-[var(--space-2)] flex items-center justify-between gap-[var(--space-3)] max-lg:hidden"
    >
      <div>
        <p
          class="font-mono text-[10px] font-bold uppercase tracking-[var(--letter-spacing-wide)] text-[var(--accent-text)]"
        >
          {{ t('integration.eyebrow') }}
        </p>
        <h2
          class="mt-0.5 text-[length:var(--text-label-sm)] font-semibold text-[var(--text-primary)]"
        >
          {{ t('integration.title') }}
        </h2>
      </div>
      <a
        v-if="showDocs"
        :href="docsLink"
        target="_blank"
        rel="noopener noreferrer"
        class="inline-flex min-h-11 shrink-0 items-center rounded-full border border-[var(--border-default)] px-3 py-1.5 text-[10px] font-semibold text-[var(--text-tertiary)] transition-colors hover:border-[var(--border-strong)] hover:text-[var(--text-primary)]"
      >
        {{ t('integration.viewAll') }}
      </a>
    </div>

    <!-- dragstart.prevent: a native image/link drag fires pointercancel, and the
         rail would stop following the pointer a few pixels into the drag. -->
    <div
      ref="wrapperRef"
      class="hero-integration-marquee-wrapper -mx-[var(--space-2)] flex min-h-[5.25rem] items-center overflow-hidden py-2 sm:min-h-[5.5rem] sm:py-4"
      :class="{ 'is-dragging': isDragging }"
      data-hero-canvas-boundary
      role="region"
      :aria-label="t('integration.title')"
      @pointerenter="onPointerEnter"
      @pointerleave="onPointerLeave"
      @pointerdown="onPointerDown"
      @pointermove="onPointerMove"
      @pointerup="onPointerUp"
      @pointercancel="onPointerCancel"
      @lostpointercapture="onLostPointerCapture"
      @dragstart.prevent
      @focusin="onFocusIn"
      @focusout="onFocusOut"
      @click.capture="onClickCapture"
    >
      <div
        ref="trackRef"
        class="flex w-max shrink-0 items-center px-[var(--space-2)] will-change-transform"
      >
        <div
          ref="firstGroupRef"
          class="flex shrink-0 items-center gap-[var(--space-3)] pr-[var(--space-3)]"
        >
          <IntegrationChip
            v-for="(tool, i) in INTEGRATIONS"
            :key="tool.id"
            :tool="tool"
            :active="i === activeIndex"
          />
        </div>
        <div
          class="flex shrink-0 items-center gap-[var(--space-3)] pr-[var(--space-3)]"
          aria-hidden="true"
        >
          <IntegrationChip
            v-for="(tool, i) in INTEGRATIONS"
            :key="`c-${tool.id}`"
            :tool="tool"
            :active="i === activeIndex"
            clone
          />
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { storeToRefs } from 'pinia'
import { useI18n } from 'vue-i18n'
import {
  useDebounceFn,
  useDocumentVisibility,
  useIntersectionObserver,
} from '@vueuse/core'
import IntegrationChip from './IntegrationChip.vue'
import { INTEGRATIONS } from '@/constants/home/integrations'
import { useAppStore } from '@/stores'

const { t } = useI18n()
const { docsLink, showDocs } = storeToRefs(useAppStore())

const wrapperRef = ref<HTMLElement | null>(null)
const trackRef = ref<HTMLElement | null>(null)
const firstGroupRef = ref<HTMLElement | null>(null)

const activeIndex = ref(-1)
const isPointerInside = ref(false)
const isFocusWithin = ref(false)
const isDragging = ref(false)

// The rail behaves like a physical instrument panel: it settles when inspected,
// then accelerates back into its ambient pace when the visitor leaves it.
// The ambient motion is a Web Animation of the track's transform, so the
// browser runs it without script; script only eases its rate, follows a drag
// and picks the chip nearest the centre.
const BASE_SPEED = 0.0252 // px / ms, matching the former 0.42 px / frame pace
const STOP_EASE_MS = 150
const RESUME_EASE_MS = 420
const DRAG_THRESHOLD = 6
// A chip crosses the centre in about 2.4s, so a highlight that trails by at
// most this long is not noticeable. The check runs only while the rail shows.
const ACTIVE_CHECK_MS = 150

let groupWidth = 0
let wrapperCenter = 0
let centers: number[] = []
let motion: Animation | null = null
let rate = 1
let easeFrame = 0
let lastEaseAt = 0
let activeTimer = 0
let suppressClickUntil = 0

interface PointerSession {
  id: number
  startX: number
  startY: number
  startOffset: number
  dragged: boolean
}

let pointerSession: PointerSession | null = null

const reduced =
  typeof window !== 'undefined' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches

const inView = ref(true)
const pageVisibility = useDocumentVisibility()
const { stop: stopViewObserver } = useIntersectionObserver(
  wrapperRef,
  (entries) => {
    inView.value = Boolean(entries.at(-1)?.isIntersecting)
  }
)

function measure() {
  const track = trackRef.value
  const wrapper = wrapperRef.value
  const group = firstGroupRef.value
  if (!track || !wrapper || !group) return

  groupWidth = group.offsetWidth
  wrapperCenter = wrapper.clientWidth / 2

  // The track and its chips move together, so these are positions along the
  // track wherever it currently is.
  const trackLeft = track.getBoundingClientRect().left
  centers = Array.from(group.children).map((el) => {
    const rect = (el as HTMLElement).getBoundingClientRect()
    return rect.left + rect.width / 2 - trackLeft
  })
}

/** How far the track has moved left, within one group width. */
function currentOffset() {
  const time = Number(motion?.currentTime ?? 0)
  return normalizeOffset(time * BASE_SPEED)
}

function setOffset(offset: number) {
  if (motion) motion.currentTime = normalizeOffset(offset) / BASE_SPEED
}

/** (Re)creates the ambient motion for the measured group width. */
function startMotion() {
  const track = trackRef.value
  if (!track || groupWidth <= 0) return
  motion?.cancel()
  motion = track.animate(
    [
      { transform: 'translateX(0)' },
      { transform: `translateX(${-groupWidth}px)` },
    ],
    { duration: groupWidth / BASE_SPEED, iterations: Infinity }
  )
  motion.playbackRate = rate
}

function updateActive() {
  if (!centers.length || groupWidth <= 0) return

  const target = normalizeOffset(wrapperCenter + currentOffset())
  let best = 0
  let bestDistance = Infinity

  for (let index = 0; index < centers.length; index += 1) {
    const center = normalizeOffset(centers[index])
    let distance = Math.abs(center - target)
    distance = Math.min(distance, groupWidth - distance)

    if (distance < bestDistance) {
      bestDistance = distance
      best = index
    }
  }

  activeIndex.value = best
}

/** Runs the highlight check while the moving rail is on screen. */
function syncActiveTimer() {
  window.clearInterval(activeTimer)
  activeTimer = 0
  if (!motion || !inView.value || pageVisibility.value === 'hidden') return
  updateActive()
  activeTimer = window.setInterval(updateActive, ACTIVE_CHECK_MS)
}

watch([inView, pageVisibility], syncActiveTimer)

function normalizeOffset(value: number) {
  if (groupWidth <= 0) return 0
  return ((value % groupWidth) + groupWidth) % groupWidth
}

function wantsToPause() {
  return isPointerInside.value || isFocusWithin.value || Boolean(pointerSession)
}

function setRate(next: number) {
  rate = next
  if (motion) motion.playbackRate = next
}

/** Eases the motion's rate toward stopped or full pace, one frame at a time. */
function easeRate(now: number) {
  easeFrame = 0
  const target = wantsToPause() ? 0 : 1
  // A frame's timestamp can precede the moment the easing was requested.
  const deltaMs = Math.min(Math.max(now - lastEaseAt, 0), 64)
  lastEaseAt = now
  const easeMs = target === 0 ? STOP_EASE_MS : RESUME_EASE_MS
  const eased = rate + (target - rate) * (1 - Math.exp(-deltaMs / easeMs))
  setRate(Math.abs(target - eased) < 0.005 ? target : eased)
  updateActive()
  if (rate !== target) easeFrame = requestAnimationFrame(easeRate)
}

function steer() {
  if (!motion || easeFrame) return
  if (rate === (wantsToPause() ? 0 : 1)) return
  lastEaseAt = performance.now()
  easeFrame = requestAnimationFrame(easeRate)
}

const onResize = useDebounceFn(() => {
  if (!motion) return
  const progress = groupWidth > 0 ? currentOffset() / groupWidth : 0
  measure()
  startMotion()
  setOffset(progress * groupWidth)
  measure()
  updateActive()
}, 180)

function onPointerEnter() {
  isPointerInside.value = true
  steer()
}

function onPointerLeave() {
  isPointerInside.value = false

  // If a non-dragging mouse press leaves the rail, it cannot produce a valid chip
  // click. Releasing this lock avoids leaving the ambient motion paused.
  if (pointerSession && !pointerSession.dragged) pointerSession = null
  steer()
}

function onPointerDown(event: PointerEvent) {
  if (event.button !== 0 || reduced) return

  pointerSession = {
    id: event.pointerId,
    startX: event.clientX,
    startY: event.clientY,
    startOffset: currentOffset(),
    dragged: false,
  }

  // Pause immediately after a real press, so a click cannot land on a different
  // chip while the browser is resolving its eventual click event.
  cancelAnimationFrame(easeFrame)
  easeFrame = 0
  setRate(0)
}

function onPointerMove(event: PointerEvent) {
  const session = pointerSession
  const wrapper = wrapperRef.value
  if (!session || session.id !== event.pointerId || !wrapper || reduced) return

  const deltaX = event.clientX - session.startX
  const deltaY = event.clientY - session.startY

  if (!session.dragged) {
    if (Math.abs(deltaX) < DRAG_THRESHOLD) return
    if (Math.abs(deltaX) <= Math.abs(deltaY)) return

    session.dragged = true
    isDragging.value = true
    suppressClickUntil = performance.now() + 450
    wrapper.setPointerCapture(event.pointerId)
  }

  event.preventDefault()
  setOffset(session.startOffset - deltaX)
  updateActive()
}

function finishPointerSession(pointerId?: number) {
  if (
    pointerSession &&
    (pointerId === undefined || pointerSession.id === pointerId)
  ) {
    if (pointerSession.dragged) suppressClickUntil = performance.now() + 450
    pointerSession = null
  }

  isDragging.value = false
  steer()
}

function onPointerUp(event: PointerEvent) {
  finishPointerSession(event.pointerId)
}

function onPointerCancel(event: PointerEvent) {
  finishPointerSession(event.pointerId)
}

function onLostPointerCapture(event: PointerEvent) {
  finishPointerSession(event.pointerId)
}

function onClickCapture(event: MouseEvent) {
  if (performance.now() < suppressClickUntil) {
    event.preventDefault()
    event.stopPropagation()
  }
}

function onFocusIn() {
  isFocusWithin.value = true
  steer()
}

function onFocusOut(event: FocusEvent) {
  const nextTarget = event.relatedTarget as Node | null
  if (!wrapperRef.value?.contains(nextTarget)) isFocusWithin.value = false
  steer()
}

function onWindowBlur() {
  finishPointerSession()
}

onMounted(() => {
  measure()

  if (reduced) {
    activeIndex.value = -1
    return
  }

  startMotion()
  syncActiveTimer()
  window.addEventListener('resize', onResize)
  window.addEventListener('blur', onWindowBlur)
})

onBeforeUnmount(() => {
  stopViewObserver()
  cancelAnimationFrame(easeFrame)
  window.clearInterval(activeTimer)
  motion?.cancel()
  motion = null
  window.removeEventListener('resize', onResize)
  window.removeEventListener('blur', onWindowBlur)
})
</script>

<style scoped>
.hero-integration-marquee-wrapper {
  -webkit-mask-image: linear-gradient(
    90deg,
    transparent,
    #000 8%,
    #000 92%,
    transparent
  );
  mask-image: linear-gradient(
    90deg,
    transparent,
    #000 8%,
    #000 92%,
    transparent
  );
  cursor: grab;
  touch-action: pan-y;
}
.hero-integration-marquee-wrapper.is-dragging {
  cursor: grabbing;
  user-select: none;
}
.hero-integration-marquee-wrapper :deep(a) {
  cursor: pointer;
}
.hero-integration-marquee-wrapper :deep(a:focus-visible) {
  outline: 2px solid var(--focus-ring);
  outline-offset: 4px;
}
</style>
