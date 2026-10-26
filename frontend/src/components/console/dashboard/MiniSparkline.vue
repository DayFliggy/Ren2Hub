<script setup lang="ts">
import { computed, useId } from 'vue'

/**
 * Decorative trend line for the KPI strip. Hand-rolled SVG rather than ECharts:
 * four of these live in one row, and four chart instances plus their resize and
 * theme observers would cost far more than the shape is worth. The figure above
 * carries the meaning, so this is aria-hidden.
 */
const props = withDefaults(
  defineProps<{
    /** A null entry is a gap: the line breaks there instead of bridging it. */
    points: (number | null)[]
    /** stroke colour — pass a CSS variable so it follows the theme */
    color?: string
    height?: number
    /**
     * Optional second series plotted on the same scale as `points` — e.g.
     * upload next to download. Normalising each series on its own would
     * stretch both to full height and erase their relative magnitude.
     */
    secondary?: (number | null)[]
    secondaryColor?: string
  }>(),
  {
    color: 'var(--accent)',
    height: 32,
    secondary: undefined,
    secondaryColor: 'var(--support)',
  }
)

const gradientId = `sparkline-${useId()}`

/** Fixed viewBox; the SVG scales to its container via width/height 100%. */
const WIDTH = 100
const PAD = 2

/** Generates smooth Bezier curve commands from an array of coordinate points */
function toSmoothLine(coords: { x: number; y: number }[]): string {
  if (coords.length < 2) return ''
  const minY = Math.min(...coords.map((point) => point.y))
  const maxY = Math.max(...coords.map((point) => point.y))
  const clampY = (value: number) => Math.min(maxY, Math.max(minY, value))
  if (coords.length === 2) {
    return `M${coords[0]!.x.toFixed(2)} ${coords[0]!.y.toFixed(2)} L${coords[1]!.x.toFixed(2)} ${coords[1]!.y.toFixed(2)}`
  }

  let d = `M${coords[0]!.x.toFixed(2)} ${coords[0]!.y.toFixed(2)}`
  for (let i = 0; i < coords.length - 1; i++) {
    const p0 = coords[i === 0 ? 0 : i - 1]!
    const p1 = coords[i]!
    const p2 = coords[i + 1]!
    const p3 = coords[i + 2] ?? p2

    // Catmull-Rom to Cubic Bezier control points
    const cp1x = p1.x + (p2.x - p0.x) / 6
    const cp1y = clampY(p1.y + (p2.y - p0.y) / 6)
    const cp2x = p2.x - (p3.x - p1.x) / 6
    const cp2y = clampY(p2.y - (p3.y - p1.y) / 6)

    d += ` C${cp1x.toFixed(2)} ${cp1y.toFixed(2)}, ${cp2x.toFixed(2)} ${cp2y.toFixed(2)}, ${p2.x.toFixed(2)} ${p2.y.toFixed(2)}`
  }
  return d
}

type Coord = { x: number; y: number }

/** Contiguous measured runs; a lone point between gaps has no line to draw. */
function segments(coords: (Coord | null)[]): Coord[][] {
  const runs: Coord[][] = [[]]
  for (const coord of coords) {
    if (coord) runs[runs.length - 1]!.push(coord)
    else if (runs[runs.length - 1]!.length) runs.push([])
  }
  return runs.filter((run) => run.length >= 2)
}

/** Normalised paths. Flat series sit on the centre line instead of the floor. */
const geometry = computed(() => {
  const values = props.points
  if (values.length < 2) return null
  const secondary =
    props.secondary && props.secondary.length > 1 ? props.secondary : null

  const measured = [...values, ...(secondary ?? [])].filter(
    (value): value is number => value !== null
  )
  if (!measured.length) return null
  const min = Math.min(...measured)
  const max = Math.max(...measured)
  const span = max - min
  const usable = props.height - PAD * 2

  const project = (series: (number | null)[]) =>
    series.map((value, i) => {
      if (value === null) return null
      const x = (i / (series.length - 1)) * WIDTH
      const ratio = span === 0 ? 0.5 : (value - min) / span
      // SVG y grows downward, so invert.
      const y = PAD + (1 - ratio) * usable
      return { x, y }
    })

  const runs = segments(project(values))
  if (!runs.length) return null
  const line = runs.map(toSmoothLine).join(' ')
  const area = runs
    .map(
      (run) =>
        `${toSmoothLine(run)} L${run[run.length - 1]!.x.toFixed(2)} ${props.height} L${run[0]!.x.toFixed(2)} ${props.height} Z`
    )
    .join(' ')
  // The beacon marks the newest sample only when that sample was measured.
  const lastRun = runs[runs.length - 1]!
  const last =
    values[values.length - 1] === null ? null : lastRun[lastRun.length - 1]!

  const secondaryRuns = secondary ? segments(project(secondary)) : []
  const secondaryLastRun = secondaryRuns[secondaryRuns.length - 1]
  return {
    line,
    area,
    last,
    secondaryLine: secondaryRuns.length
      ? secondaryRuns.map(toSmoothLine).join(' ')
      : null,
    secondaryLast:
      secondaryLastRun && secondary![secondary!.length - 1] !== null
        ? secondaryLastRun[secondaryLastRun.length - 1]!
        : null,
  }
})
</script>

<template>
  <svg
    v-if="geometry"
    :viewBox="`0 0 ${WIDTH} ${height}`"
    preserveAspectRatio="none"
    class="w-full transition-all duration-300"
    :style="{ height: `${height / 16}rem` }"
    aria-hidden="true"
  >
    <defs>
      <linearGradient :id="gradientId" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0%" :stop-color="color" stop-opacity="0.22" />
        <stop offset="60%" :stop-color="color" stop-opacity="0.06" />
        <stop offset="100%" :stop-color="color" stop-opacity="0" />
      </linearGradient>
    </defs>
    <path :d="geometry.area" :fill="`url(#${gradientId})`" />
    <path
      :d="geometry.line"
      fill="none"
      :stroke="color"
      stroke-width="1.8"
      stroke-linecap="round"
      stroke-linejoin="round"
      vector-effect="non-scaling-stroke"
      class="drop-shadow-[0_1px_3px_var(--chart-line-glow)]"
    />
    <!-- Line only for the second series: one shaded band is enough. Dashed, so
         the two series differ by more than colour. -->
    <path
      v-if="geometry.secondaryLine"
      :d="geometry.secondaryLine"
      fill="none"
      :stroke="secondaryColor"
      stroke-width="1.8"
      stroke-dasharray="3 2.5"
      stroke-linecap="round"
      stroke-linejoin="round"
      vector-effect="non-scaling-stroke"
      class="drop-shadow-[0_1px_3px_var(--chart-line-glow)]"
    />
    <!-- Latest point with a pulse beacon; it glows at night only. -->
    <circle
      v-if="geometry.last"
      :cx="geometry.last.x"
      :cy="geometry.last.y"
      r="2.5"
      :fill="color"
      class="sparkline-beacon animate-pulse-slow"
      :style="{ '--beacon-color': color }"
    />
    <circle
      v-if="geometry.secondaryLast"
      :cx="geometry.secondaryLast.x"
      :cy="geometry.secondaryLast.y"
      r="2.5"
      :fill="secondaryColor"
      class="sparkline-beacon animate-pulse-slow"
      :style="{ '--beacon-color': secondaryColor }"
    />
  </svg>
</template>

<style scoped>
/* Chart glow is a night-theme accent, like --chart-line-glow; day stays flat. */
html.dark .sparkline-beacon {
  filter: drop-shadow(0 0 5px var(--beacon-color));
}
</style>
