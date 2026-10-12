<script setup lang="ts">
import {
  computed,
  nextTick,
  onMounted,
  ref,
  useId,
  watch,
  type CSSProperties,
} from 'vue'
import { useI18n } from 'vue-i18n'

import { useTableScrollFade } from '@/composables/useTableScrollFade'
import {
  formatUsageDate,
  formatUsageMetric,
  shiftUsageDate,
  usageHeatStyle,
  usageWeekdayLabels,
  type UsageDistributionMetric,
  type UsageDistributionPeriod,
  type UsageDistributionView,
  type UsageHeatmapCell,
} from '@/utils/usageDistribution'

const props = defineProps<{
  view: UsageDistributionView
  period: UsageDistributionPeriod
  metric: UsageDistributionMetric
  metricColor: string
  periodLabel: string
  metricLabel: string
}>()

/** The month calendar always reserves six weeks so its height never jumps. */
const MONTH_SLOTS = 42

const { t, locale } = useI18n()
const root = ref<HTMLElement | null>(null)
const scroller = ref<HTMLElement | null>(null)
const focusDate = ref('')
const tooltipId = useId()
const tooltip = ref<{
  date: string
  label: string
  left: number
  top: number
} | null>(null)
const dragging = ref(false)
let dragStartX = 0
let dragStartScroll = 0
let suppressClick = false

const { canScrollLeft, canScrollRight, updateScrollFade } =
  useTableScrollFade(scroller)
const canDrag = computed(() => canScrollLeft.value || canScrollRight.value)

const weekdays = computed(() => usageWeekdayLabels(locale.value))
const gridLabel = computed(() =>
  t('dashboard.distribution.gridLabel', {
    period: props.periodLabel,
    metric: props.metricLabel,
  })
)

const visibleCells = computed(() => {
  return props.view.cells.flatMap((cell, index) => {
    if (!cell.inRange || cell.future) return []

    let column: number
    let row: number
    if (props.period === 'month') {
      column = (index % 7) + 1
      row = Math.floor(index / 7) + 1
    } else {
      column = Math.floor(index / 7) + 1
      row = (index % 7) + 1
    }
    return [{ cell, column, row }]
  })
})

// Grid positions outside the 30-day range (leading days, the rest of this
// week and the padded sixth week) render as quiet, non-interactive slots.
const monthPlaceholders = computed(() =>
  Array.from({ length: MONTH_SLOTS }, (_, index) => index).flatMap((index) => {
    const cell = props.view.cells[index]
    if (cell?.inRange && !cell.future) return []
    return [
      { key: index, column: (index % 7) + 1, row: Math.floor(index / 7) + 1 },
    ]
  })
)
const monthMarkers = computed(() => {
  const weeks = Array.from({ length: props.view.weekCount }, (_, index) =>
    props.view.cells.slice(index * 7, index * 7 + 7)
  )
  const markers = weeks.flatMap((week, index) => {
    const monthStart = week.find((cell) => cell.date.endsWith('-01'))
    const source =
      monthStart ?? (index === 0 ? week.find((cell) => cell.inRange) : null)
    if (!source) return []
    const [year, month] = source.monthKey.split('-').map(Number)
    return [
      {
        key: source.monthKey,
        column: index + 1,
        label: new Intl.DateTimeFormat(locale.value, {
          month: 'short',
        }).format(new Date(year!, month! - 1, 1)),
      },
    ]
  })

  return (
    markers
      .map((marker, index) => ({
        ...marker,
        span:
          (markers[index + 1]?.column ?? props.view.weekCount + 1) -
          marker.column,
      }))
      // Fitted year cells are ~10px wide: a partial month at either edge with
      // a one-week span would collide with its neighbour or overflow the grid.
      .filter((marker) => marker.span >= 2)
  )
})

function dayNumber(date: string): number {
  return Number(date.slice(8))
}

function denseWeekdayLabel(index: number): string {
  if (props.period !== 'year') return weekdays.value[index]!
  return index % 2 === 0 ? weekdays.value[index]! : ''
}

function cellLabel(cell: UsageHeatmapCell): string {
  return t('dashboard.distribution.cellLabel', {
    date: formatUsageDate(cell.date, locale.value),
    metric: props.metricLabel,
    value: formatUsageMetric(props.metric, cell.value),
  })
}
function showTooltip(event: FocusEvent | MouseEvent, cell: UsageHeatmapCell) {
  if (!root.value) return
  const target = event.currentTarget as HTMLElement
  const targetRect = target.getBoundingClientRect()
  const rootRect = root.value.getBoundingClientRect()
  tooltip.value = {
    date: cell.date,
    label: cellLabel(cell),
    left: Math.min(
      rootRect.width - 84,
      Math.max(84, targetRect.left - rootRect.left + targetRect.width / 2)
    ),
    top: targetRect.top - rootRect.top - 6,
  }
}

function hideTooltip(date: string) {
  if (tooltip.value?.date === date) tooltip.value = null
}

function onCellFocus(event: FocusEvent, cell: UsageHeatmapCell) {
  focusDate.value = cell.date
  showTooltip(event, cell)
}

async function focusCell(date: string) {
  const target = root.value?.querySelector<HTMLButtonElement>(
    `[data-usage-date="${date}"]`
  )
  if (!target) return
  focusDate.value = date
  await nextTick()
  target.focus()
}

function onCellKeydown(event: KeyboardEvent, cell: UsageHeatmapCell) {
  const active = visibleCells.value
  if (event.key === 'Home' || event.key === 'End') {
    event.preventDefault()
    const target = event.key === 'Home' ? active[0] : active.at(-1)
    if (target) void focusCell(target.cell.date)
    return
  }

  let days: number
  if (event.key === 'ArrowUp') days = -7
  else if (event.key === 'ArrowDown') days = 7
  else if (event.key === 'ArrowLeft') days = props.period === 'month' ? -1 : -7
  else if (event.key === 'ArrowRight') days = props.period === 'month' ? 1 : 7
  else return

  event.preventDefault()
  void focusCell(shiftUsageDate(cell.date, days))
}

async function alignLatest() {
  await nextTick()
  if (scroller.value) scroller.value.scrollLeft = scroller.value.scrollWidth
  // Content width changes on period switches do not resize the scroller, and
  // a clamped scrollLeft does not always emit a scroll event.
  updateScrollFade()
}
function onScrollPointerDown(event: PointerEvent) {
  const target = event.currentTarget as HTMLElement
  if (event.button !== 0 || target.scrollWidth <= target.clientWidth) return
  dragging.value = true
  suppressClick = false
  dragStartX = event.clientX
  dragStartScroll = target.scrollLeft
  target.setPointerCapture?.(event.pointerId)
}

function onScrollPointerMove(event: PointerEvent) {
  if (!dragging.value) return
  const target = event.currentTarget as HTMLElement
  const distance = event.clientX - dragStartX
  if (Math.abs(distance) > 3) suppressClick = true
  target.scrollLeft = dragStartScroll - distance
  event.preventDefault()
}

function onScrollPointerEnd(event: PointerEvent) {
  if (!dragging.value) return
  const target = event.currentTarget as HTMLElement
  dragging.value = false
  if (target.hasPointerCapture?.(event.pointerId)) {
    target.releasePointerCapture(event.pointerId)
  }
}

function onScrollClickCapture(event: MouseEvent) {
  if (!suppressClick) return
  event.preventDefault()
  event.stopPropagation()
  suppressClick = false
}

watch(
  () => [props.view, props.period] as const,
  () => {
    const active = visibleCells.value
    if (!active.some((entry) => entry.cell.date === focusDate.value)) {
      focusDate.value = active.at(-1)?.cell.date ?? ''
    }
    tooltip.value = null
    void alignLatest()
  },
  { immediate: true }
)

onMounted(() => void alignLatest())
</script>
<template>
  <div
    ref="root"
    class="usage-heatmap relative min-w-0"
    :data-usage-layout="period"
  >
    <div v-if="period === 'month'" class="usage-month-calendar">
      <div class="usage-calendar-weekdays" aria-hidden="true">
        <span v-for="(weekday, index) in weekdays" :key="index">
          {{ weekday }}
        </span>
      </div>
      <div class="usage-month-grid" role="group" :aria-label="gridLabel">
        <button
          v-for="{ cell, column, row } in visibleCells"
          :key="cell.date"
          type="button"
          class="usage-month-cell focus-ring"
          :style="[
            usageHeatStyle(cell.level, metricColor),
            { gridColumn: column, gridRow: row },
          ]"
          :tabindex="focusDate === cell.date ? 0 : -1"
          :aria-label="cellLabel(cell)"
          :aria-describedby="
            tooltip?.date === cell.date ? tooltipId : undefined
          "
          :data-usage-date="cell.date"
          :data-usage-level="cell.level"
          @focus="onCellFocus($event, cell)"
          @blur="hideTooltip(cell.date)"
          @mouseenter="showTooltip($event, cell)"
          @mouseleave="hideTooltip(cell.date)"
          @keydown="onCellKeydown($event, cell)"
        >
          {{ dayNumber(cell.date) }}
        </button>
        <span
          v-for="placeholder in monthPlaceholders"
          :key="`placeholder-${placeholder.key}`"
          class="usage-month-placeholder"
          :style="{ gridColumn: placeholder.column, gridRow: placeholder.row }"
          aria-hidden="true"
          data-usage-placeholder
        />
      </div>
    </div>
    <div
      v-else
      class="usage-dense"
      :data-period="period"
      :style="{ '--usage-weeks': view.weekCount } as CSSProperties"
    >
      <div class="flex min-w-0 gap-[var(--usage-label-gap)]">
        <div class="usage-dense-weekdays" aria-hidden="true">
          <span v-for="(_, index) in weekdays" :key="index">
            {{ denseWeekdayLabel(index) }}
          </span>
        </div>
        <div class="relative -m-1 min-w-0 flex-1">
          <div
            ref="scroller"
            class="usage-dense-scroll overflow-x-auto p-1"
            :class="{ 'is-draggable': canDrag, 'is-dragging': dragging }"
            data-usage-scroll
            :data-usage-draggable="canDrag || undefined"
            @pointerdown="onScrollPointerDown"
            @pointermove="onScrollPointerMove"
            @pointerup="onScrollPointerEnd"
            @pointercancel="onScrollPointerEnd"
            @click.capture="onScrollClickCapture"
          >
            <div class="usage-dense-content">
              <div class="usage-dense-months" aria-hidden="true">
                <span
                  v-for="marker in monthMarkers"
                  :key="marker.key"
                  class="usage-dense-month"
                  :style="{
                    gridColumn: `${marker.column} / span ${marker.span}`,
                  }"
                  :data-usage-month="marker.label"
                >
                  {{ marker.label }}
                </span>
              </div>
              <div
                class="usage-dense-grid"
                role="group"
                :aria-label="gridLabel"
              >
                <button
                  v-for="{ cell, column, row } in visibleCells"
                  :key="cell.date"
                  type="button"
                  class="usage-dense-cell focus-ring"
                  :style="[
                    usageHeatStyle(cell.level, metricColor),
                    { gridColumn: column, gridRow: row },
                  ]"
                  :tabindex="focusDate === cell.date ? 0 : -1"
                  :aria-label="cellLabel(cell)"
                  :aria-describedby="
                    tooltip?.date === cell.date ? tooltipId : undefined
                  "
                  :data-usage-date="cell.date"
                  :data-usage-level="cell.level"
                  @focus="onCellFocus($event, cell)"
                  @blur="hideTooltip(cell.date)"
                  @mouseenter="showTooltip($event, cell)"
                  @mouseleave="hideTooltip(cell.date)"
                  @keydown="onCellKeydown($event, cell)"
                />
              </div>
            </div>
          </div>
          <div
            v-if="canScrollLeft"
            class="table-scroll-fade table-scroll-fade--start"
            data-usage-scroll-fade="start"
            aria-hidden="true"
          />
          <div
            v-if="canScrollRight"
            class="table-scroll-fade"
            data-usage-scroll-fade="end"
            aria-hidden="true"
          />
        </div>
      </div>
      <p
        v-if="canDrag"
        class="usage-drag-hint"
        aria-hidden="true"
        data-usage-drag-hint
      >
        <svg
          width="10"
          height="10"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2.5"
          aria-hidden="true"
        >
          <path d="m15 18-6-6 6-6" />
        </svg>
        {{ t('dashboard.distribution.dragHint') }}
      </p>
    </div>

    <div
      v-if="tooltip"
      :id="tooltipId"
      class="pointer-events-none absolute z-20 -translate-x-1/2 -translate-y-full whitespace-nowrap rounded-lg border border-[var(--border-subtle)] bg-[var(--surface-overlay)] px-2.5 py-1.5 text-[11px] text-[var(--text-primary)] shadow-[var(--overlay-shadow)]"
      :style="{ left: `${tooltip.left}px`, top: `${tooltip.top}px` }"
      role="tooltip"
    >
      {{ tooltip.label }}
    </div>
  </div>
</template>

<style scoped>
/*
 * Height contract. Below sm every period stacks the heatmap above the same
 * analytics block, so the root reserves the six-week month calendar height and
 * dense periods centre inside it. From sm the card stage owns the height (see
 * UsageDistributionCard) and the root shrinks to its content. Day cells are
 * 24px tall, the WCAG 2.2 minimum target size; the 2px row gap keeps the six
 * weeks (169px with the weekday header) within the 170px stage.
 */
.usage-heatmap {
  --usage-calendar-width: 32px;
  --usage-calendar-height: 24px;
  --usage-calendar-column-gap: 3px;
  --usage-calendar-row-gap: 2px;
  --usage-calendar-header: 15px;
  display: flex;
  min-height: calc(
    var(--usage-calendar-header) + 6 * var(--usage-calendar-height) + 5 *
      var(--usage-calendar-row-gap)
  );
  flex-direction: column;
  justify-content: center;
}

/* Year cells are sized from the card width (cqi), never from their column.
   Quarter columns stretch inside the grid instead and need no container. */
.usage-heatmap[data-usage-layout='year'] {
  container-type: inline-size;
}

.usage-month-calendar {
  width: calc(
    7 * var(--usage-calendar-width) + 6 * var(--usage-calendar-column-gap)
  );
}

.usage-calendar-weekdays,
.usage-month-grid {
  display: grid;
  grid-template-columns: repeat(7, var(--usage-calendar-width));
  column-gap: var(--usage-calendar-column-gap);
}

.usage-calendar-weekdays {
  height: var(--usage-calendar-header);
  text-align: center;
  font-size: 9px;
  line-height: 11px;
  color: var(--text-tertiary);
}
.usage-month-grid {
  grid-template-rows: repeat(6, var(--usage-calendar-height));
  row-gap: var(--usage-calendar-row-gap);
}

.usage-month-cell,
.usage-month-placeholder {
  border: 1px solid;
  border-radius: 6px;
}

.usage-month-cell {
  display: flex;
  align-items: flex-start;
  justify-content: flex-start;
  padding: 3px 5px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 9px;
  line-height: 1;
  transition:
    transform 120ms ease,
    border-color 120ms ease,
    background-color 120ms ease;
}

.usage-month-placeholder {
  border-style: dashed;
  border-color: var(--border-subtle);
}

.usage-dense {
  --usage-label-width: 12px;
  --usage-label-gap: 6px;
  --usage-axis-height: 14px;
}

/* Quarter cells keep a fixed height and stretch their width with the grid up
   to twice that height, so the weeks fill the space beside the analytics. */
.usage-dense[data-period='quarter'] {
  --usage-column-gap: 3px;
  --usage-row-gap: 3px;
  --usage-cell-height: 14px;
  --usage-columns: repeat(var(--usage-weeks), minmax(12px, 28px));
}

/* Year cells stay square and fill the card width exactly with every week
   visible; phones keep them readable and pan through the weeks instead. */
.usage-dense[data-period='year'] {
  --usage-column-gap: 2px;
  --usage-row-gap: 2px;
  --usage-fit-cell: calc(
    (
        100cqi - var(--usage-label-width) - var(--usage-label-gap) -
          (var(--usage-weeks) - 1) * var(--usage-column-gap)
      ) /
      var(--usage-weeks)
  );
  --usage-cell-size: 14px;
  --usage-cell-height: var(--usage-cell-size);
  --usage-columns: repeat(var(--usage-weeks), var(--usage-cell-size));
}

.usage-dense-weekdays {
  display: grid;
  width: var(--usage-label-width);
  flex: none;
  grid-template-rows: repeat(7, var(--usage-cell-height));
  row-gap: var(--usage-row-gap);
  padding-top: calc(var(--usage-axis-height) + var(--usage-row-gap));
  font-size: 9px;
  line-height: 1;
  color: var(--text-tertiary);
}

.usage-dense-weekdays > span {
  display: flex;
  align-items: center;
  justify-content: center;
}
/* Year keeps its fitted width; quarter content sizes from its grid tracks. */
.usage-dense[data-period='year'] .usage-dense-content {
  width: calc(
    var(--usage-weeks) * (var(--usage-cell-size) + var(--usage-column-gap)) -
      var(--usage-column-gap)
  );
}

.usage-dense-months,
.usage-dense-grid {
  display: grid;
  grid-template-columns: var(--usage-columns);
  column-gap: var(--usage-column-gap);
}

.usage-dense-months {
  height: var(--usage-axis-height);
  margin-bottom: var(--usage-row-gap);
}

.usage-dense-month {
  padding-left: 2px;
  font-size: 9px;
  line-height: 11px;
  white-space: nowrap;
  color: var(--text-tertiary);
}

.usage-dense-grid {
  grid-template-rows: repeat(7, var(--usage-cell-height));
  row-gap: var(--usage-row-gap);
}

.usage-dense-cell {
  display: block;
  width: 100%;
  height: var(--usage-cell-height);
  border: 1px solid;
  border-radius: 4px;
  transition:
    transform 120ms ease,
    border-color 120ms ease,
    background-color 120ms ease;
}

.usage-dense[data-period='year'] .usage-dense-cell {
  border-radius: 3px;
}

/* The 4px padding keeps focus rings and hover lift inside the scroll clip. */
.usage-dense-scroll {
  scrollbar-width: none;
}

.usage-dense-scroll::-webkit-scrollbar {
  display: none;
}

.usage-dense-scroll.is-draggable {
  cursor: grab;
  touch-action: pan-y;
  user-select: none;
}

.usage-dense-scroll.is-dragging {
  cursor: grabbing;
}

.usage-drag-hint {
  display: flex;
  align-items: center;
  gap: 2px;
  margin-top: 4px;
  padding-left: calc(var(--usage-label-width) + var(--usage-label-gap));
  font-size: 10px;
  line-height: 14px;
  color: var(--text-tertiary);
}

.usage-month-cell:hover,
.usage-month-cell:focus-visible,
.usage-dense-cell:hover,
.usage-dense-cell:focus-visible {
  position: relative;
  z-index: 1;
  transform: translateY(-1px);
}

@media (max-width: 359px) {
  .usage-heatmap {
    --usage-calendar-width: 30px;
  }

  /* 320px phones leave ~13.5px columns; keep quarter cells no taller. */
  .usage-dense[data-period='quarter'] {
    --usage-cell-height: 13px;
  }
}

@media (min-width: 640px) {
  .usage-heatmap {
    --usage-calendar-width: 42px;
    --usage-calendar-column-gap: 5px;
    min-height: 0;
  }

  .usage-month-cell,
  .usage-month-placeholder {
    border-radius: 7px;
  }

  .usage-month-cell {
    padding: 4px 6px;
    font-size: 10px;
  }

  /* Seven 16px rows with 4px gaps (154px with the month axis) stay within
     the 162px stacked and 170px desktop stage budgets. The wider column gap
     keeps each week reading as one column. */
  .usage-dense[data-period='quarter'] {
    --usage-column-gap: 5px;
    --usage-row-gap: 4px;
    --usage-cell-height: 16px;
    --usage-columns: repeat(var(--usage-weeks), minmax(12px, 32px));
  }

  /* Year spans the stage with every week visible (about 8px cells on a 640px
     tablet), capped by the stage height; only phones pan through the weeks. */
  .usage-dense[data-period='year'] {
    --usage-cell-size: min(var(--usage-fit-cell), 12px);
  }
}
</style>
