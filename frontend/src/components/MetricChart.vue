<script setup lang="ts">
// Filled line chart for the dashboard System metrics dashlet (port of the
// legacy dashlets/templates/metrics.htm Chart.js line charts: teal stroke,
// filled area, hidden x-axis, y starting at zero).
import { computed } from 'vue'

const props = defineProps<{
  /** label is the series name shown as the chart legend. */
  label: string
  /** values is the rolling series, oldest first (max 15 points). */
  values: number[]
  /** times are the matching HH:MM stamps, used as point tooltips. */
  times?: string[]
}>()

// Fixed drawing space; the SVG stretches to the container width and the
// stroke keeps its width thanks to vector-effect.
const W = 300
const H = 60

/** top is the y-axis maximum, rounded up so the line never touches the edge. */
const top = computed(() => {
  const max = Math.max(0, ...props.values)
  return max <= 0 ? 1 : max * 1.1
})

const points = computed(() => {
  const n = props.values.length
  if (n === 0) return []
  return props.values.map((v, i) => ({
    x: n === 1 ? W / 2 : (i / (n - 1)) * W,
    y: H - (Math.max(0, v) / top.value) * H,
    v,
    t: props.times?.[i] ?? '',
  }))
})

const line = computed(() => points.value.map((p) => `${p.x.toFixed(1)},${p.y.toFixed(1)}`).join(' '))

/** area closes the polyline down to the baseline for the fill. */
const area = computed(() => {
  const p = points.value
  if (!p.length) return ''
  return `${p[0].x.toFixed(1)},${H} ${line.value} ${p[p.length - 1].x.toFixed(1)},${H}`
})

/** formatted keeps the axis label short (2 decimals only when needed). */
function fmt(v: number): string {
  return v >= 100 ? String(Math.round(v)) : String(Math.round(v * 100) / 100)
}
</script>

<template>
  <figure class="border border-border bg-surface p-2" :data-test="`metric-chart`">
    <figcaption class="mb-1 text-center text-xs font-bold text-text">{{ label }}</figcaption>
    <div class="flex items-stretch gap-1">
      <div class="flex w-10 shrink-0 flex-col justify-between text-right text-[10px] text-text-muted">
        <span>{{ fmt(top) }}</span>
        <span>0</span>
      </div>
      <svg
        class="h-16 w-full"
        :viewBox="`0 0 ${W} ${H}`"
        preserveAspectRatio="none"
        role="img"
        :aria-label="label"
      >
        <polygon v-if="area" :points="area" fill="rgba(75, 192, 192, 0.2)" />
        <polyline
          v-if="line"
          :points="line"
          fill="none"
          stroke="rgb(75, 192, 192)"
          stroke-width="1"
          vector-effect="non-scaling-stroke"
        />
        <title v-if="points.length">
          {{ points.map((p) => `${p.t} ${fmt(p.v)}`).join(' · ') }}
        </title>
      </svg>
    </div>
  </figure>
</template>
